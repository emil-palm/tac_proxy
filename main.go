package main

import (
	"fmt"
	"github.com/VividCortex/godaemon"
	flag "github.com/spf13/pflag"
	"github.com/spf13/viper"
	"io/ioutil"
	"log"
	"log/syslog"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"tictac"
	"unicode/utf8"
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
	var config_test *bool = flag.Bool("configtest", false, "Test configuration and exit")
	var daemon *bool = flag.Bool("daemon", false, "Daemonize the tac_proxy (implicit start)")
	var stopdaemon *bool = flag.Bool("stop-daemon", false, "Stops the daemon")

	// Parse flags
	flag.Parse()

	// Now add config flag if needed
	if utf8.RuneCountInString(*config_name) > 0 {
		viper.SetConfigFile(*config_name)
	}

	// Add defaults to viper.
	viper.SetDefault("port", 49)
	viper.SetDefault("mattermost.webhook.enable", false)

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

	configerr := viper.ReadInConfig()
	if configerr != nil {
		log.Println(configerr)
		os.Exit(1)
	}
	if *config_test {
		os.Exit(0)
	}

	_, err = os.Stat(viper.GetString("pidfile"))
	if *daemon {
		godaemon.MakeDaemon(&godaemon.DaemonAttr{})
		log.SetOutput(logger)
		log.SetFlags(0)
	} else if *stopdaemon {
		if os.IsNotExist(err) {
			log.Printf("Cannot stop daemon %s is missing\n", viper.GetString("pidfile"))
			os.Exit(1)
		} else {
			dat, err := ioutil.ReadFile(viper.GetString("pidfile"))
			if err != nil {
				log.Printf("%s", err)
				os.Exit(1)
			}
			pid, _ := strconv.Atoi(strings.Trim(string(dat), "\n"))
			process, err := os.FindProcess(pid)
			if err != nil {
				log.Printf("%s", err)
				os.Exit(1)
			}
			err = process.Kill()
			if err != nil {
				log.Printf("%s", err)
				os.Exit(1)
			}

			log.Printf("Killed process: %d", pid)
			os.Remove(viper.GetString("pidfile"))
			os.Exit(0)
		}

	}

	if os.IsNotExist(err) == false {
		os.Remove(viper.GetString("pidfile"))
	}

	err = ioutil.WriteFile(viper.GetString("pidfile"), []byte(fmt.Sprintf("%d\n", os.Getpid())), os.ModeTemporary)

	if err != nil {
		log.Println(fmt.Sprintf("Could not create pid file %s", viper.GetString("pidfile")))
		os.Exit(1)
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGHUP)

	go func() {
		for _ = range c {
			viper.ReadInConfig()
			log.Println("Reloading config")
		}
	}()

	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	if viper.Get("address") == nil {
		fmt.Println("No address given in configuration cannot continue")
		os.Exit(1)
	}

	log.Printf("Now accepting connections on %s:%d", viper.GetString("address"), viper.Get("port"))
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
