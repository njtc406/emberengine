package event

import (
	"crypto/tls"
	"strconv"

	"github.com/nats-io/nats.go"
	"github.com/njtc406/emberengine/engine/pkg/config"
	"github.com/njtc406/emberengine/engine/pkg/def"
)

func switchOpts(conf *config.NatsConf) []nats.Option {
	var opts []nats.Option
	if conf != nil {
		if conf.MaxReconnects == 0 {
			conf.MaxReconnects = def.NatsDefaultMaxReconnects
		}
		opts = append(opts, nats.MaxReconnects(conf.MaxReconnects))

		if conf.ReconnectWait == 0 {
			conf.ReconnectWait = def.NatsDefaultReconnectWait
		}
		opts = append(opts, nats.ReconnectWait(conf.ReconnectWait))

		if conf.PingInterval == 0 {
			conf.PingInterval = def.NatsDefaultPingInterval
		}
		opts = append(opts, nats.PingInterval(conf.PingInterval))

		if conf.PingMaxOutstanding == 0 {
			conf.PingMaxOutstanding = def.NatsDefaultPingMaxOutstanding
		}
		opts = append(opts, nats.MaxPingsOutstanding(conf.PingMaxOutstanding))

		if conf.ReconnectBufSize == 0 {
			conf.ReconnectBufSize = def.NatsDefaultReconnectBufSize
		}
		opts = append(opts, nats.ReconnectBufSize(conf.ReconnectBufSize))

		if conf.Token != "" {
			opts = append(opts, nats.Token(conf.Token))
		} else {
			if conf.UserName != "" {
				opts = append(opts, nats.UserInfo(conf.UserName, conf.Password))
			}
		}

		if shouldEnableNatsTLS(conf.Secure) {
			tlsConfig := &tls.Config{
				MinVersion:         tls.VersionTLS12,
				InsecureSkipVerify: conf.InsecureSkipVerify,
			}
			if conf.TLSServerName != "" {
				tlsConfig.ServerName = conf.TLSServerName
			}
			opts = append(opts, nats.Secure(tlsConfig))
		}

		if conf.CAs != "" {
			opts = append(opts, nats.RootCAs(conf.CAs))
		}

		if conf.Cert != "" && conf.CertKey != "" {
			opts = append(opts, nats.ClientCert(conf.Cert, conf.CertKey))
		}
	}
	return opts
}

func shouldEnableNatsTLS(secure string) bool {
	if secure == "" {
		return false
	}
	b, err := strconv.ParseBool(secure)
	if err == nil {
		return b
	}
	return true
}

func (eb *Bus) applySubPendingLimits(subscription *nats.Subscription) {
	if subscription == nil {
		return
	}
	msgLimit := eb.subPendingMsgLimit
	bytesLimit := eb.subPendingBytesLimit
	if msgLimit <= 0 {
		msgLimit = def.NatsDefaultSubPendingMsgLimit
	}
	if bytesLimit <= 0 {
		bytesLimit = def.NatsDefaultSubPendingBytesLimit
	}
	_ = subscription.SetPendingLimits(msgLimit, bytesLimit)
}

func (eb *Bus) addSub(key string, sub *nats.Subscription) {
	eb.subMap.Store(key, sub)
}

func (eb *Bus) loadAndDelSub(key string) (*nats.Subscription, bool) {
	sub, ok := eb.subMap.LoadAndDelete(key)
	if !ok {
		return nil, false
	}
	return sub.(*nats.Subscription), true
}

func (eb *Bus) unSubscribe(key string) {
	// 没有订阅者了,那么取消监听
	if eb.isNatsEnabled() {
		if subscription, ok := eb.loadAndDelSub(key); ok {
			if err := subscription.Unsubscribe(); err != nil {
				eb.Errorf("unsubscribe global event error: %v", err)
			}
		}
	}
}
