package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

var Rules []Rule
var ConfigFileName = "rules.json"
var SimultaneousConnections = make([]int, 0)
var Verbose = false

const Version = "0.2.0 / Build 3"

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
	Rules = conf.Rules
	SimultaneousConnections = make([]int, len(Rules))

	//Start listeners
	for index := range Rules {
		go func(i int) {
			if Rules[i].Quota < 0 { //If the quota is already reached why listen for connections?
				return
			}
			fmt.Println("Forwarding from", Rules[i].Listen, "port to", Rules[i].Forward)
			ln, err := net.Listen("tcp", ":"+strconv.Itoa(int(Rules[i].Listen))) //Listen on port
			if err != nil {
				panic(err)
			}

			for {
				conn, err := ln.Accept() //The loop will be held here
				if Rules[i].Quota < 0 {
					fmt.Println("Quota reached for port", Rules[i].Forward, "pointing to", Rules[i].Forward)
					if err == nil {
						_ = conn.Close()
					}
					saveConfig(conf)
					break
				}
				if err != nil {
					println("Error on accepting connection:", err.Error())
					continue
				}
				go handleRequest(conn, i)
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
	fmt.Println("Exiting")
}

func saveConfig(config Config) {
	config.Rules = Rules
	b, err := json.Marshal(config)
	if err != nil {
		fmt.Println("Error parsing rules: ", err)
		return
	}
	err = ioutil.WriteFile(ConfigFileName, b, 0644)
	if err != nil {
		fmt.Println("Error re-writing rules: ", err)
	}
	if Verbose {
		fmt.Println("Saved the config file at ", time.Now().Format("2006-01-02 15:04:05"))
	}
}

func handleRequest(conn net.Conn, index int) {
	if Rules[index].Simultaneous != 0 && SimultaneousConnections[index] >= (Rules[index].Simultaneous*2) { //If we have reached quota just terminate the connection; 0 means no limits
		if Verbose {
			fmt.Println("Blocking new connection for port", Rules[index].Listen, "because the connection limit is reached. The current active connections count is", SimultaneousConnections[index]/2)
		}
		_ = conn.Close()
		return
	}

	proxy, err := net.Dial("tcp", Rules[index].Forward) //Open a connection to remote host
	if err != nil {
		println("Error on dialing remote host:", err.Error())
		_ = conn.Close()
		return
	}

	SimultaneousConnections[index] += 2 //Two is added; One for client to server and another for server to client

	go copyIO(conn, proxy, index)
	go copyIO(proxy, conn, index)
}

func copyIO(src, dest net.Conn, index int) {
	defer src.Close()
	defer dest.Close()
	r, _ := io.Copy(src, dest) //r is the amount of bytes transferred
	Rules[index].Quota -= r
	SimultaneousConnections[index]-- //This will actually run twice
}
