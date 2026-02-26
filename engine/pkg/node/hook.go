package node

import "github.com/njtc406/emberengine/engine/pkg/utils/translate"

type HookFun func(map[any]any)

type StartParam struct {
	Language translate.LanguageType // 语言
	Version  string
	ConfPath string
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
