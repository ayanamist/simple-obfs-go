package main

import (
	"bufio"
	"bytes"
	cryptoRand "crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	mathRand "math/rand"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

func main() {
	remoteHost := os.Getenv("SS_REMOTE_HOST")
	remotePort := os.Getenv("SS_REMOTE_PORT")
	localHost := os.Getenv("SS_LOCAL_HOST")
	localPort := os.Getenv("SS_LOCAL_PORT")
	pluginOpts := os.Getenv("SS_PLUGIN_OPTIONS")
	if remoteHost == "" || remotePort == "" || localHost == "" || localPort == "" {
		log.Fatalln("some must-have variables are empty")
	}
	opts := make(map[string]string)
	for _, l := range strings.Split(pluginOpts, ";") {
		kv := strings.Split(l, "=")
		if len(kv) > 1 {
			opts[kv[0]] = kv[1]
		} else {
			opts[l] = ""
		}
	}
	obfsType := opts["obfs"]
	if obfsType != "http" {
		log.Fatalf("obfs=%s not supported", obfsType)
	}
	obfsHost := opts["obfs-host"]
	if obfsHost == "" {
		log.Fatalln("obfs-host not specified")
	}

	localAddr := fmt.Sprintf("%s:%s", localHost, localPort)
	remoteAddr := fmt.Sprintf("%s:%s", remoteHost, remotePort)
	l, err := net.Listen("tcp", localAddr)
	if err != nil {
		log.Fatalf("listen %s: %v", localAddr, err)
	}

	timeout := 5 * time.Second
	if s, ok := opts["timeout"]; ok {
		if t, err := strconv.Atoi(s); err == nil {
			timeout = time.Duration(t) * time.Second
		}
	}
	dialer := &net.Dialer{
		Timeout: timeout,
	}

	b := make([]byte, 8)
	cryptoRand.Read(b)
	rand := mathRand.New(mathRand.NewSource(int64(binary.BigEndian.Uint64(b))))

	for {
		localConn, err := l.Accept()
		if err != nil {
			log.Printf("accept: %v", err)
			continue
		}
		go func() {
			defer localConn.Close()
			remoteConn, err := dialer.Dial("tcp", remoteAddr)
			if err != nil {
				log.Printf("dial %s: %v", remoteAddr, err)
				return
			}
			defer remoteConn.Close()
			buf := make([]byte, 8192)
			n, err := localConn.Read(buf)
			if err != nil {
				log.Printf("read %s: %v", localConn.LocalAddr(), err)
				return
			}
			b := make([]byte, 16)
			rand.Read(b)
			req, _ := http.NewRequest("GET", fmt.Sprintf("http://%s/", obfsHost), bytes.NewBuffer(buf[:n]))
			req.Header.Set("User-Agent", fmt.Sprintf("curl/7.%d.%d", rand.Int()%51, rand.Int()%2))
			req.Header.Set("Upgrade", "websocket")
			req.Header.Set("Connection", "Upgrade")
			req.Header.Set("Sec-WebSocket-Key", base64.URLEncoding.EncodeToString(b))
			req.ContentLength = int64(n)
			req.Host = obfsHost

			if err := req.Write(remoteConn); err != nil {
				log.Printf("write http req %s: %v", localConn.LocalAddr(), err)
				return
			}
			go func() {
				defer localConn.Close()
				defer remoteConn.Close()
				reader := bufio.NewReader(remoteConn)
				if _, err := http.ReadResponse(reader, req); err != nil {
					log.Printf("read http resp %s: %v", localConn.LocalAddr(), err)
					return
				}
				io.Copy(localConn, reader)
			}()
			io.Copy(remoteConn, localConn)
		}()
	}
}
