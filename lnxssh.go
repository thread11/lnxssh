package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"runtime/debug"
	"strings"

	"github.com/gorilla/websocket"
	"golang.org/x/crypto/ssh"
)

var SETTINGS = struct {
	VERSION string
	DEBUG   bool
}{
	VERSION: "20220619",
	DEBUG:   false,
}

func Skip(err error) {
	if err != nil {
		log.Println(err)
		log.Println("skip error")
	}
}

func Throw(err error) {
	if err != nil {
		panic(err)
	}
}

func Catch() {
	var err interface{}
	err = recover()
	if err != nil {
		log.Println(err)
		log.Println(string(debug.Stack()))
	}
}

func IndexHandler(response http.ResponseWriter, request *http.Request) {
	var err error

	var HTML string
	HTML = ""

	var tpl *template.Template
	if HTML == "" {
		tpl, err = template.ParseFiles("template/index.html")
		Skip(err)
	} else {
		tpl, err = template.New("X").Parse(HTML)
		Skip(err)
	}

	var data = map[string]interface{}{}
	tpl.Execute(response, data)
}

// https://pkg.go.dev/github.com/gorilla/websocket#hdr-Overview
// https://pkg.go.dev/golang.org/x/crypto/ssh#Session.RequestPty
func WsHandler(response http.ResponseWriter, request *http.Request) {
	var err error

	var host string
	var port string
	var user string
	var password string

	host = request.FormValue("host")
	port = request.FormValue("port")
	user = request.FormValue("user")
	password = request.FormValue("password")

	host = strings.TrimSpace(host)
	port = strings.TrimSpace(port)
	user = strings.TrimSpace(user)
	password = strings.TrimSpace(password)

	log.Println("host:", host)
	log.Println("port:", port)
	log.Println("user:", user)

	if host == "" {
		host = "127.0.0.1"
	}
	if port == "" {
		port = "22"
	}
	if user == "" {
		user = "root"
	}

	log.Printf("ssh %s@%s:%s\n", user, host, port)

	var upgrader = websocket.Upgrader{}
	upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}

	var ws *websocket.Conn
	ws, err = upgrader.Upgrade(response, request, nil)
	if ws != nil {
		defer ws.Close()
	}
	Throw(err)

	var auth []ssh.AuthMethod
	if password != "" {
		log.Println("use password")
		auth = []ssh.AuthMethod{ssh.Password(password)}
	} else {
		log.Println("use pubkey")

		var pem_bytes []byte
		pem_bytes, err = ioutil.ReadFile(os.Getenv("HOME") + "/.ssh/id_rsa")
		Throw(err)

		var signer ssh.Signer
		signer, err = ssh.ParsePrivateKey(pem_bytes)
		Throw(err)

		auth = []ssh.AuthMethod{ssh.PublicKeys(signer)}
	}

	var addr string
	addr = fmt.Sprintf("%s:%s", host, port)

	var config *ssh.ClientConfig
	config = &ssh.ClientConfig{
		User:            user,
		Auth:            auth,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	var ssh_client *ssh.Client
	ssh_client, err = ssh.Dial("tcp", addr, config)
	if ssh_client != nil {
		defer ssh_client.Close()
	}
	if err != nil {
		ws.WriteMessage(websocket.TextMessage, []byte(err.Error()))
	}
	Throw(err)

	var ssh_session *ssh.Session
	ssh_session, err = ssh_client.NewSession()
	if ssh_session != nil {
		defer ssh_session.Close()
	}
	Throw(err)

	var ssh_stdin io.WriteCloser
	ssh_stdin, err = ssh_session.StdinPipe()
	if ssh_stdin != nil {
		defer ssh_stdin.Close()
	}
	Throw(err)

	var ssh_stdout io.Reader
	ssh_stdout, err = ssh_session.StdoutPipe()
	Throw(err)

	var ssh_stderr io.Reader
	ssh_stderr, err = ssh_session.StderrPipe()
	Throw(err)

	var modes ssh.TerminalModes
	modes = ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}

	// Requesting a Pseudo-Terminal
	// ok, err := s.ch.SendRequest("pty-req", true, Marshal(&req))
	// https://datatracker.ietf.org/doc/html/rfc4254#section-6.2
	err = ssh_session.RequestPty("xterm", 40, 80, modes)
	Throw(err)

	// Starting a Shell or a Command
	// ok, err := s.ch.SendRequest("shell", true, nil)
	// https://datatracker.ietf.org/doc/html/rfc4254#section-6.5
	err = ssh_session.Shell()
	Throw(err)

	// ssh_stdout -> websocket
	go func() {
		defer Catch()

		var buf []byte
		buf = make([]byte, 4096)

		for {
			var len int
			len, err = ssh_stdout.Read(buf)
			Throw(err)

			if SETTINGS.DEBUG {
				fmt.Print(string(buf[:len]))
			} else {
				log.Printf("stdout, %d bytes\n", len)
			}

			ws.WriteMessage(websocket.TextMessage, buf[:len])
		}
	}()

	// ssh_stderr -> websocket
	go func() {
		defer Catch()

		var buf []byte
		buf = make([]byte, 4096)

		for {
			var len int
			len, err = ssh_stderr.Read(buf)
			Throw(err)

			if SETTINGS.DEBUG {
				fmt.Print(string(buf[:len]))
			} else {
				log.Printf("stderr, %d bytes\n", len)
			}

			ws.WriteMessage(websocket.TextMessage, buf[:len])
		}
	}()

	// websocket -> ssh_stdin
	for {
		log.Println("stdin")

		var msg []byte
		_, msg, err = ws.ReadMessage()
		Throw(err)

		var data map[string]interface{}
		json.Unmarshal(msg, &data)

		log.Printf("%+v\n", data)

		var action int64
		action = int64(data["action"].(float64))

		switch action {
		case 1:
			var cmd string
			cmd = data["cmd"].(string)

			_, err = ssh_stdin.Write([]byte(cmd))
			Throw(err)
		case 2:
			var rows int64
			var cols int64

			rows = int64(data["rows"].(float64))
			cols = int64(data["cols"].(float64))

			err = ssh_session.WindowChange(int(rows), int(cols))
			Throw(err)
		}
	}
}

func SetupRoutes() {
	http.HandleFunc("/", IndexHandler)
	http.HandleFunc("/ws", WsHandler)

	fs := http.FileServer(http.Dir("./static/"))
	http.Handle("/static/", http.StripPrefix("/static/", fs))
}

func main() {
	defer Catch()

	log.SetFlags(log.LstdFlags | log.Lshortfile)

	var debug bool
	flag.BoolVar(&debug, "debug", false, "Debug")
	flag.Parse()
	log.Println("debug:", debug)
	SETTINGS.DEBUG = debug
	log.Printf("SETTINGS: %+v\n", SETTINGS)

	log.Println("Hello WebSocket")

	SetupRoutes()

	log.Fatal(http.ListenAndServe(":1234", nil))
}
