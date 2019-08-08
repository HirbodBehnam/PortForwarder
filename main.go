package main

import (
	"encoding/json"
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
const Version  = "0.1.0 / Build 1"

type Rule struct {
	Listen	int
	Forward string
	Quota 	int64
}
type Config struct {
	SaveDuration int
	Rules[]      Rule
}

func main() {
	{//Parse arguments
		args := os.Args
		if len(args) > 1 && (args[1] == "-v" || args[1] == "-V" || args[1] == "--version") {
			fmt.Println("Created by Hirbod Behnam")
			fmt.Println("Source at https://github.com/HirbodBehnam/PortForwarder")
			fmt.Println("To use a custom file as config, just pass it to program")
			fmt.Println("Version",Version)
			os.Exit(0)
		}
		if len(args) > 1{
			ConfigFileName = args[1]
		}
	}
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


	for index := range Rules {
		go func(i int) {
			if Rules[i].Quota < 0 {
				return
			}
			ln, err := net.Listen("tcp",":" + strconv.Itoa(Rules[i].Listen))
			if err != nil {
				panic(err)
			}
			for {
				conn, err := ln.Accept()
				if Rules[i].Quota < 0 {
					fmt.Println("Quota reached for port",Rules[i].Forward,"pointing to",Rules[i].Forward,"Quota is now ",Rules[i].Quota)
					if err == nil {
						_ = conn.Close()
					}
					saveConfig(conf)
					break
				}
				if err != nil {
					println("Error on accepting connection:",err.Error())
					continue
				}
				go handleRequest(conn,i)
			}
		}(index)
	}
	go func() {//Save file every ten minutes
		for {
			time.Sleep(time.Duration(conf.SaveDuration) * time.Second)
			saveConfig(conf)
		}
	}()

	//https://gobyexample.com/signals
	sigs := make(chan os.Signal, 1)
	done := make(chan bool, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigs
		fmt.Println()
		fmt.Println(sig)
		done <- true
	}()
	fmt.Println("Ctrl + C to stop")
	<-done
	fmt.Println("exiting")
}

func saveConfig(config Config) {
	config.Rules = Rules
	b, err := json.Marshal(config)
	if err != nil {
		fmt.Println("Error parsing rules: ", err)
		return
	}
	err = ioutil.WriteFile(ConfigFileName,b,0644)
	if err != nil {
		fmt.Println("Error re-writing rules: ", err)
	}
	fmt.Println("Saved")
}

func handleRequest(conn net.Conn, index int) {
	proxy, err := net.Dial("tcp", Rules[index].Forward)
	if err != nil {
		println("Error on dialing remote host:",err.Error())
		_ = conn.Close()
		return
	}

	go copyIO(conn, proxy, index)
	go copyIO(proxy, conn,index)
}

func copyIO(src, dest net.Conn,index int) {
	defer src.Close()
	defer dest.Close()
	r,_ := io.Copy(src, dest)
	Rules[index].Quota -= r
}
