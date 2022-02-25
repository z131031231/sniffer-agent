package capture

import (
	log "github.com/sirupsen/logrus"
	sd "sniffer-agent/session-dealer"
	"math/rand"
	"time"
)

var (
	localIPAddr *string  //本机IP地址

	sessionPool = make(map[string]sd.ConnSession) //线程池
	// sessionPoolLock sync.Mutex
)

func init() {
	ipAddr, err := getLocalIPAddr()
	if err != nil {
		panic(err)
	}

	localIPAddr = &ipAddr
	log.Infof("parsed local ip address:%s", *localIPAddr)

	rand.Seed(time.Now().UnixNano())
}
