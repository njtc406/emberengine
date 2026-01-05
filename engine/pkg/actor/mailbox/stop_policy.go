package mailbox

import "strings"

// DrainPolicy 控制 Mailbox 停机时对剩余队列消息的处理方式。
type DrainPolicy int32

const (
	// DrainExecute 继续执行剩余消息（保持现有语义）。
	DrainExecute DrainPolicy = iota
	// DrainDiscard 丢弃剩余消息（仅回收与触发 OnComplete，不执行业务）。
	DrainDiscard
)

func ParseDrainPolicy(v string) DrainPolicy {
	s := strings.TrimSpace(strings.ToLower(v))
	switch s {
	case "discard", "drop", "skip":
		return DrainDiscard
	case "execute", "drain", "run", "":
		return DrainExecute
	default:
		return DrainExecute
	}
}
