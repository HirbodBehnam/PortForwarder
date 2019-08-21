package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"
)

var Rules CSafeRule
var ConfigFileName = "rules.json"
var SimultaneousConnections CSafeConnections
var Verbose = false

const Version = "1.0.0 / Build 4"

type CSafeConnections struct {
	SimultaneousConnections []int
	mu                      sync.RWMutex
}

type CSafeRule struct {
	Rules []Rule
	mu    sync.RWMutex
}

type Rule struct {
	Listen       uint16
	Forward      string
	Quota        int64
	Simultaneous int
}
type Config struct {
	SaveDuration int
	Rules        []Rule
}

func main() {
	{ //Parse arguments
		configFileName := flag.String("config", "rules.json", "The config filename")
		verbose := flag.Bool("v", false, "Verbose mode")
		help := flag.Bool("h", false, "Show help")
		flag.Parse()

		Verbose = *verbose
		if Verbose {
			fmt.Println("Verbose mode on")
		}
		ConfigFileName = *configFileName

		if *help {
			fmt.Println("Created by Hirbod Behnam")
			fmt.Println("Source at https://github.com/HirbodBehnam/PortForwarder")
			fmt.Println("Version", Version)
			flag.PrintDefaults()
			os.Exit(0)
		}
	}

	//Read config file
	confF, err := ioutil.ReadFile(ConfigFileName)
	if err != nil {
		panic("Cannot read the config file. (io Error) " + err.Error())
	}
	var conf Config
	err = json.Unmarshal(confF, &conf)
	if err != nil {
		panic("Cannot read the config file. (Parse Error) " + err.Error())
	}
	Rules.Rules = conf.Rules
	SimultaneousConnections.SimultaneousConnections = make([]int, len(Rules.Rules))

	//Start listeners
	for index := range Rules.Rules {
		go func(i int) {
			Rules.mu.RLock()           //Lock it and clone it
			loopRule := Rules.Rules[i] //Clone it
			Rules.mu.RUnlock()         //Let the other goroutines use it
			if loopRule.Quota < 0 {    //If the quota is already reached why listen for connections?
				return
			}
			log.Println("Forwarding from", loopRule.Listen, "port to", loopRule.Forward)
			ln, err := net.Listen("tcp", ":"+strconv.Itoa(int(loopRule.Listen))) //Listen on port
			if err != nil {
				panic(err)
			}

			for {
				conn, err := ln.Accept() //The loop will be held here
				Rules.mu.RLock()         //Lock the mutex to just read the quota
				if Rules.Rules[i].Quota < 0 {
					Rules.mu.RUnlock()
					log.Println("Quota reached for port", loopRule.Forward, "pointing to", loopRule.Forward)
					if err == nil {
						_ = conn.Close()
					}
					saveConfig(conf)
					break
				}
				Rules.mu.RUnlock()
				if err != nil {
					println("Error on accepting connection:", err.Error())
					continue
				}
				go handleRequest(conn, i, loopRule)
			}
		}(index)
	}

	//Save config file
	go func() {
		for {
			time.Sleep(time.Duration(conf.SaveDuration) * time.Second) //Save file every x seconds
			saveConfig(conf)
		}
	}()

	//https://gobyexample.com/signals
	sigs := make(chan os.Signal, 1)
	done := make(chan bool, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() { //This will wait for a signal
		<-sigs
		done <- true
	}()
	fmt.Println("Ctrl + C to stop")
	<-done

	saveConfig(conf) //Save the config file one last time before exiting
	log.Println("Exiting")
}

func saveConfig(config Config) {
	Rules.mu.RLock() //Lock to clone the rules
	config.Rules = Rules.Rules
	Rules.mu.RUnlock()
	b, err := json.Marshal(config)
	if err != nil {
		log.Println("Error parsing rules: ", err)
		return
	}
	err = ioutil.WriteFile(ConfigFileName, b, 0644)
	if err != nil {
		log.Println("Error re-writing rules: ", err)
	}
	if Verbose {
		log.Println("Saved the config")
	}
}

func handleRequest(conn net.Conn, index int, r Rule) {
	//Send a clone of rules to here to avoid need of locking mutex
	SimultaneousConnections.mu.RLock()
	if r.Simultaneous != 0 && SimultaneousConnections.SimultaneousConnections[index] >= (r.Simultaneous*2) { //If we have reached quota just terminate the connection; 0 means no limits
		if Verbose {
			log.Println("Blocking new connection for port", r.Listen, "because the connection limit is reached. The current active connections count is", SimultaneousConnections.SimultaneousConnections[index]/2)
		}
		SimultaneousConnections.mu.RUnlock()
		_ = conn.Close()
		return
	}
	SimultaneousConnections.mu.RUnlock()

	proxy, err := net.Dial("tcp", r.Forward) //Open a connection to remote host
	if err != nil {
		log.Println("Error on dialing remote host:", err.Error())
		_ = conn.Close()
		return
	}

	SimultaneousConnections.mu.Lock()
	SimultaneousConnections.SimultaneousConnections[index] += 2 //Two is added; One for client to server and another for server to client
	SimultaneousConnections.mu.Unlock()

	go copyIO(conn, proxy, index)
	go copyIO(proxy, conn, index)
}

func copyIO(src, dest net.Conn, index int) {
	defer src.Close()
	defer dest.Close()

	r, _ := io.Copy(src, dest) //r is the amount of bytes transferred

	Rules.mu.Lock() //Lock to read the amount of data transferred
	Rules.Rules[index].Quota -= r
	Rules.mu.Unlock()

	SimultaneousConnections.mu.Lock()
	SimultaneousConnections.SimultaneousConnections[index]-- //This will actually run twice
	SimultaneousConnections.mu.Unlock()
}
