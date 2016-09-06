package main

import (
	"fmt"
	"net"
	"os"
	"unicode/utf8"
	"tictac"
	"github.com/spf13/viper"
	flag "github.com/spf13/pflag"
	"github.com/VividCortex/godaemon"
	"log"
	"log/syslog"
	"os/signal"
	"syscall"
	"io/ioutil"
)

func main() {

	// Configure viper

	// Search in /etc/tac_proxy and current folder
	viper.AddConfigPath("/etc/tac_proxy/")
	viper.AddConfigPath(".")
	// Its required to be named tac_proxy
	viper.SetConfigName("tac_proxy")
	// And type is yaml
	viper.SetConfigType("yaml")

	// Add flag for config
	var config_name *string = flag.String("config", "", "Specifies a alternate config file to use")
	var daemon *bool = flag.Bool("daemon", false, "Daemonize the tac_proxy")
	// Parse flags
	flag.Parse()

	// Now add config flag if needed
	if utf8.RuneCountInString(*config_name) > 0 {
		viper.SetConfigFile(*config_name)
	}

	// Add defaults to viper.
	viper.SetDefault("port",49)
	viper.SetDefault("mattermost.webhook.enable",false)

	// Setup logger

	var loglevel = syslog.LOG_INFO
	switch viper.GetString("syslog.level") {
		case "emerg":
		loglevel = syslog.LOG_EMERG
		case "alert":
		loglevel = syslog.LOG_ALERT
		case "crit":
		loglevel = syslog.LOG_CRIT
		case "err":
		loglevel = syslog.LOG_ERR
		case "warning":
		loglevel = syslog.LOG_WARNING
		case "notice":
		loglevel = syslog.LOG_NOTICE
		case "info":
		default:
		loglevel = syslog.LOG_INFO
		case "debug":
		loglevel = syslog.LOG_DEBUG
	}

	logger, err := syslog.New(loglevel, "tac-proxy")
	defer logger.Close()
	if err != nil {
		log.Println("Could not setup syslog")
	}

	err = viper.ReadInConfig()
	if *daemon {
		godaemon.MakeDaemon(&godaemon.DaemonAttr{})
		log.SetOutput(logger)
		log.SetFlags(0)
	}
	_, err = os.Stat(viper.GetString("pidfile"))
	if os.IsNotExist(err) == false {
		log.Printf("%s already exists, aborting\n", viper.GetString("pidfile"))
		os.Exit(1)
	}
	defer os.Remove(viper.GetString("pidfile"))
	err = ioutil.WriteFile(viper.GetString("pidfile"),[]byte(fmt.Sprintf("%s\n",os.Getpid())), os.ModeTemporary)


	if err != nil {
		log.Println(fmt.Sprintf("Could not create pid file %s", viper.GetString("pidfile")))
		os.Exit(1)
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGHUP)

	go func(){
		for _ = range c {
			viper.ReadInConfig()
			log.Println("Reloading config")
		}
	}()

	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	if ( viper.Get("address") == nil ) {
		fmt.Println("No address given in configuration cannot continue")
		os.Exit(1)
	}

	ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", viper.Get("address"), viper.Get("port")))
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	for {
		conn, err := ln.Accept()
		if err != nil {
			fmt.Println("Error!")
			continue
		}
		s := tictac.NewSession(conn)
		go s.Handle(logger)
	}

}
