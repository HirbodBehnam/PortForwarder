package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Thread safe rules
var Rules CSafeRule

// The config file name that has the default of rules.json
var ConfigFileName = "rules.json"

// Thread safe simultaneous connections
var SimultaneousConnections CSafeConnections

// Log level (higher = more logs)
var Verbose = 1

// If true that program will save the config file just before it exits
var SaveBeforeExit = true

// The version of program
const Version = "1.4.1 / Build 12"

// A struct to count the active connections on each port
type CSafeConnections struct {
	SimultaneousConnections []int
	mu                      sync.RWMutex
}

// Thread safe rules to read and write it
type CSafeRule struct {
	Rules []Rule
	mu    sync.RWMutex
}

// The main rule struct
type Rule struct {
	// Name does not have any effect on anything
	Name string
	// The port to listen on
	Listen uint16
	// The destination to forward the packets to
	Forward string
	// The remaining bytes that the user can use
	// Note that this variable is int64 not uint64. Because if it was uin64, it would overflow to 2^64-1.
	Quota int64
	// The last time this port can be accessed in UTC
	// 0 means that this rules does not expire
	ExpireDate int64
	// Number of simultaneous connections allowed * 2
	// 0 means that there is no limit
	Simultaneous int
}

// The config file struct
type Config struct {
	// Interval of saving files in seconds
	SaveDuration int
	// The timeout of all connections
	// Values equal or lower than 0 disable the timeout
	Timeout int64
	// All of the forwarding rules
	Rules []Rule
}

// Is timeout enabled?
var EnableTimeOut = true

// The timout value in time.Duration type
var TimeoutDuration time.Duration

func main() {
	{ // Parse arguments
		flag.StringVar(&ConfigFileName, "config", "rules.json", "The config filename")
		flag.IntVar(&Verbose, "verbose", 1, "Verbose level: 0->None(Mostly Silent), 1->Quota reached, expiry date and typical errors, 2->Connection flood 3->Timeout drops 4->All logs and errors")
		help := flag.Bool("h", false, "Show help")
		flag.BoolVar(&SaveBeforeExit, "no-exit-save", false, "Set this argument to disable the save of rules before exiting")
		flag.Parse()

		if *help {
			fmt.Println("Created by Hirbod Behnam")
			fmt.Println("Source at https://github.com/HirbodBehnam/PortForwarder")
			fmt.Println("Version", Version)
			flag.PrintDefaults()
			os.Exit(0)
		}

		if Verbose != 0 {
			fmt.Println("Verbose mode on level", Verbose)
		}

		SaveBeforeExit = !SaveBeforeExit
	}

	// Read config file
	var conf Config
	{
		confF, err := ioutil.ReadFile(ConfigFileName)
		if err != nil {
			panic("Cannot read the config file. (io Error) " + err.Error())
		}

		err = json.Unmarshal(confF, &conf)
		if err != nil {
			panic("Cannot read the config file. (Parse Error) " + err.Error())
		}

		Rules.Rules = conf.Rules
		SimultaneousConnections.SimultaneousConnections = make([]int, len(Rules.Rules))
		if conf.Timeout <= 0 {
			logVerbose(1, "Disabled timeout")
			EnableTimeOut = false
		} else {
			TimeoutDuration = time.Duration(conf.Timeout) * time.Second
			logVerbose(3, "Set timeout to", TimeoutDuration)
		}
	}

	// Start listeners
	for index, rule := range Rules.Rules {
		go func(i int, loopRule Rule) {
			if loopRule.Quota < 0 { // If the quota is already reached why listen for connections?
				log.Println("Skip enabling forward on port", loopRule.Listen, "because the quota is reached.")
				return
			}
			if loopRule.ExpireDate != 0 && loopRule.ExpireDate < time.Now().Unix() { // Same thing goes with expire date
				log.Println("Skip enabling forward on port", loopRule.Listen, "because this rule is expired.")
				return
			}

			log.Println("Forwarding from", loopRule.Listen, "port to", loopRule.Forward)
			ln, err := net.Listen("tcp", ":"+strconv.Itoa(int(loopRule.Listen))) // Initialize the listener
			if err != nil {
				panic(err) // This will terminate the program
			}

			for {
				conn, err := ln.Accept() // The loop will be held here;

				Rules.mu.RLock()              // Lock the rules mutex to read the quota and expire date
				if Rules.Rules[i].Quota < 0 { // Check the quota
					Rules.mu.RUnlock()
					logVerbose(1, "Quota reached for port", loopRule.Listen, "pointing to", loopRule.Forward)
					if err == nil {
						_ = conn.Close()
					}
					saveConfig(conf) // Force write the config file
					break
				}
				if Rules.Rules[i].ExpireDate != 0 && Rules.Rules[i].ExpireDate < time.Now().Unix() { // Check expire date
					Rules.mu.RUnlock()
					logVerbose(1, "Expire date reached for port", loopRule.Listen, "pointing to", loopRule.Forward)
					if err == nil {
						_ = conn.Close()
					}
					saveConfig(conf) // Force write the config file
					break
				}
				Rules.mu.RUnlock()

				if err != nil {
					logVerbose(1, "Error on accepting connection:", err.Error())
					continue
				}

				go handleRequest(conn, i, loopRule)
			}
		}(index, rule)
	}

	// Save config file in intervals
	go func() {
		sd := conf.SaveDuration
		if sd == 0 {
			sd = 600
			conf.SaveDuration = 600
		}
		saveInterval := time.Duration(sd) * time.Second
		for {
			time.Sleep(saveInterval) // Save file every x seconds
			saveConfig(conf)
		}
	}()

	// https://gobyexample.com/signals
	sigs := make(chan os.Signal, 1)
	done := make(chan bool, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() { //This will wait for a signal
		<-sigs
		done <- true
	}()
	log.Println("Ctrl + C to stop")
	<-done
	if SaveBeforeExit {
		saveConfig(conf) // Save the config file one last time before exiting
	}
	log.Println("Exiting")
}

// Saves the config file
func saveConfig(config Config) {
	Rules.mu.RLock() //Lock to read the rules
	config.Rules = Rules.Rules
	b, _ := json.Marshal(config)
	Rules.mu.RUnlock()

	err := ioutil.WriteFile(ConfigFileName, b, 0644)
	if err != nil {
		logVerbose(1, "Error re-writing rules: ", err)
	} else {
		logVerbose(4, "Saved the config")
	}
}

// All incoming connections end up here
// Index is the rule index
func handleRequest(conn net.Conn, index int, r Rule) {
	// Send a clone of rules to here to avoid need of locking mutex
	SimultaneousConnections.mu.RLock()
	if r.Simultaneous != 0 && SimultaneousConnections.SimultaneousConnections[index] >= (r.Simultaneous*2) { //If we have reached quota just terminate the connection; 0 means no limits
		logVerbose(2, "Blocking new connection for port", r.Listen, "because the connection limit is reached. The current active connections count is", SimultaneousConnections.SimultaneousConnections[index]/2)
		SimultaneousConnections.mu.RUnlock()
		_ = conn.Close()
		return
	}
	SimultaneousConnections.mu.RUnlock()

	// Open a connection to remote host
	proxy, err := net.Dial("tcp", r.Forward)
	if err != nil {
		logVerbose(1, "Error on dialing remote host:", err.Error())
		_ = conn.Close()
		return
	}

	// Increase the connection count
	SimultaneousConnections.mu.Lock()
	SimultaneousConnections.SimultaneousConnections[index] += 2 // Two is added; One for client to server and another for server to client
	logVerbose(4, "Accepting a connection from", conn.RemoteAddr(), "; Now", SimultaneousConnections.SimultaneousConnections[index], "SimultaneousConnections")
	SimultaneousConnections.mu.Unlock()

	go copyIO(conn, proxy, index) // client -> server
	go copyIO(proxy, conn, index) // server -> client
}

// Copies the src to dest
// Index is the rule index
func copyIO(src, dest net.Conn, index int) {
	defer src.Close()
	defer dest.Close()

	// r is the amount of bytes transferred
	var r int64
	var err error

	if EnableTimeOut {
		r, err = copyBuffer(dest, src)
	} else {
		r, err = io.Copy(dest, src) // if timeout is not enabled just use the original io.copy
	}

	if err != nil {
		if strings.Contains(err.Error(), "i/o timeout") {
			logVerbose(3, "A connection timed out from", src.RemoteAddr(), "to", dest.RemoteAddr())
		} else if strings.HasPrefix(err.Error(), "cannot set timeout for") {
			if strings.HasSuffix(err.Error(), "use of closed network connection") {
				logVerbose(4, err.Error())
			} else {
				logVerbose(1, err.Error())
			}
		} else {
			logVerbose(4, "Error on copyBuffer:", err.Error())
		}
	}

	Rules.mu.Lock() // lock to change the amount of data transferred
	Rules.Rules[index].Quota -= r
	Rules.mu.Unlock()

	SimultaneousConnections.mu.Lock()
	SimultaneousConnections.SimultaneousConnections[index]-- // this will run twice
	logVerbose(4, "Closing a connection from", src.RemoteAddr(), "; Connections Now:", SimultaneousConnections.SimultaneousConnections[index])
	SimultaneousConnections.mu.Unlock()
}

func copyBuffer(dst, src net.Conn) (written int64, err error) {
	buf := make([]byte, 32768) // 32kb buffer
	for {
		err = src.SetDeadline(time.Now().Add(TimeoutDuration))
		if err != nil {
			err = errors.New("cannot set timeout for src: " + err.Error())
			break
		}
		nr, er := src.Read(buf)
		if nr > 0 {
			err = dst.SetDeadline(time.Now().Add(TimeoutDuration))
			if err != nil {
				err = errors.New("cannot set timeout for dest: " + err.Error())
				break
			}
			nw, ew := dst.Write(buf[0:nr])
			if nw > 0 {
				written += int64(nw)
			}
			if ew != nil {
				err = ew
				break
			}
			if nr != nw {
				err = io.ErrShortWrite
				break
			}
		}
		if er != nil {
			if er != io.EOF {
				err = er
			}
			break
		}
	}
	return written, err
}

func logVerbose(level int, msg ...interface{}) {
	if Verbose >= level {
		log.Println(msg)
	}
}
