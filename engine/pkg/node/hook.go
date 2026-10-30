package node

import (
	"github.com/njtc406/emberengine/v2/engine/pkg/config"
	"github.com/njtc406/emberengine/v2/engine/pkg/utils/translate"

	inf "github.com/njtc406/emberengine/v2/engine/pkg/interfaces"
)

type HookFun func(ctx inf.INodeContext, extra map[any]any) error

type StartParam struct {
	Language translate.LanguageType // 语言
	Version  string
	ConfPath string
	Conf     *config.Config
	Hooks    []HookFun
	Extra    map[any]any
}

type StartOption func(*StartParam)

func WithLanguage(language translate.LanguageType) StartOption {
	return func(p *StartParam) {
		p.Language = language
	}
}

func WithVersion(v string) StartOption {
	return func(p *StartParam) {
		p.Version = v
	}
}

// WithConf 使用外部配置，跳过配置文件和远程配置加载。nil 保留文件加载行为。
// 配置由 Node 直接持有，启动后调用方不应再修改或供其他 Node 共用。
func WithConf(conf *config.Config) StartOption {
	return func(p *StartParam) {
		p.Conf = conf
	}
}

func WithConfPath(confPath string) StartOption {
	return func(p *StartParam) {
		p.ConfPath = confPath
	}
}

func WithHooks(hooks ...HookFun) StartOption {
	return func(p *StartParam) {
		p.Hooks = hooks
	}
}

func WithExtra(extra map[any]any) StartOption {
	return func(p *StartParam) {
		p.Extra = extra
	}
}
