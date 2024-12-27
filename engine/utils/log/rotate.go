package log

import (
	"github.com/lestrrat-go/file-rotatelogs"
	"runtime"
	"time"
)

type options struct {
	// Pattern 文件切分精度 可选(%Y%m%d%H%M) 默认(%Y%m%d)
	// 具体含义github.com/lestrrat-go/strftime/specifications.go的defaultSpecifications中定义
	Pattern string

	// MaxAge 日志存留时间
	// 默认:15*24*time.Hour
	MaxAge time.Duration

	// RotationTime 日志切分时间
	// 默认:24*time.Hour
	// 这个值和上面的pattern一起,可以实时对文件进行切分 比如:这里配置1分钟切一次,那么如果pattern精确到分钟时,就会每分钟产生一个新的文件
	RotationTime time.Duration
}

func (o *options) opts(p string, opts ...ROption) []rotatelogs.Option {
	for _, opt := range opts {
		opt(o)
	}
	l := make([]rotatelogs.Option, 0, 3)
	if runtime.GOOS != `windows` {
		l = append(l, rotatelogs.WithLinkName(p))
	}
	l = append(l, rotatelogs.WithMaxAge(o.MaxAge))
	l = append(l, rotatelogs.WithRotationTime(o.RotationTime))
	return l
}

type Rotate = rotatelogs.RotateLogs

type ROption func(o *options)

// WithPattern sets the Rotate pattern.
func WithPattern(s string) ROption {
	return func(o *options) {
		o.Pattern = s
	}
}

// WithMaxAge sets the Rotate max_age.
func WithMaxAge(d time.Duration) ROption {
	return func(o *options) {
		o.MaxAge = d
	}
}

// WithRotationTime sets the Rotate rotation time.
func WithRotationTime(d time.Duration) ROption {
	return func(o *options) {
		o.RotationTime = d
	}
}

func defaultOpt() *options {
	return &options{
		Pattern:      `_%Y%m%d.log`,
		MaxAge:       15 * 24 * time.Hour,
		RotationTime: 24 * time.Hour,
	}
}

// New creates a new Rotate object.
// When using this rotation object, it is recommended to turn off the log lock(rusLogger.SetNoLock())
func rotateNew(p string, opts ...ROption) (*Rotate, error) {
	opt := defaultOpt()
	optList := opt.opts(p, opts...)
	return rotatelogs.New(
		p+opt.Pattern,
		optList...,
	)
}
