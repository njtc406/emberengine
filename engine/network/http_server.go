package network

import (
	"crypto/tls"
	"errors"
	"github.com/njtc406/emberengine/engine/utils/log"
	"net/http"
	"time"
)

var DefaultMaxHeaderBytes int = 1 << 20

type CAFile struct {
	CertFile string
	Keyfile  string
}

type HttpServer struct {
	listenAddr   string
	readTimeout  time.Duration
	writeTimeout time.Duration

	handler    http.Handler
	caFileList []CAFile

	httpServer *http.Server
}

func (slf *HttpServer) Init(listenAddr string, handler http.Handler, readTimeout time.Duration, writeTimeout time.Duration) {
	slf.listenAddr = listenAddr
	slf.handler = handler
	slf.readTimeout = readTimeout
	slf.writeTimeout = writeTimeout
}

func (slf *HttpServer) Start() {
	go slf.startListen()
}

func (slf *HttpServer) startListen() error {
	if slf.httpServer != nil {
		return errors.New("Duplicate start not allowed")
	}

	var tlsCaList []tls.Certificate
	var tlsConfig *tls.Config
	for _, caFile := range slf.caFileList {
		cer, err := tls.LoadX509KeyPair(caFile.CertFile, caFile.Keyfile)
		if err != nil {
			log.SysLogger.Fatalf("Load CA file is fail, errdef:%s, certFile:%s, keyFile:%s", err.Error(), caFile.CertFile, caFile.Keyfile)
			return err
		}
		tlsCaList = append(tlsCaList, cer)
	}

	if len(tlsCaList) > 0 {
		tlsConfig = &tls.Config{Certificates: tlsCaList}
	}

	slf.httpServer = &http.Server{
		Addr:           slf.listenAddr,
		Handler:        slf.handler,
		ReadTimeout:    slf.readTimeout,
		WriteTimeout:   slf.writeTimeout,
		MaxHeaderBytes: DefaultMaxHeaderBytes,
		TLSConfig:      tlsConfig,
	}

	var err error
	if len(tlsCaList) > 0 {
		err = slf.httpServer.ListenAndServeTLS("", "")
	} else {
		err = slf.httpServer.ListenAndServe()
	}

	if err != nil {
		log.SysLogger.Fatalf("Listen failure, errdef:%s", err.Error())
		return err
	}

	return nil
}

func (slf *HttpServer) SetCAFile(caFile []CAFile) {
	slf.caFileList = caFile
}
