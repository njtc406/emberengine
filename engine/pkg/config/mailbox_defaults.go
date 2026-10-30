package config

const (
	// DefaultMailboxQueueCapacity is the per-Worker, per-physical-queue slot count.
	DefaultMailboxQueueCapacity = 4096
	// DefaultMailboxQueueStorageBytes limits queue layout across the maximum Worker count, not RSS.
	DefaultMailboxQueueStorageBytes uint64 = 64 << 20
)
