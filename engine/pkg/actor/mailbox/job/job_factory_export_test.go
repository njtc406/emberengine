package job

// resetFactoryFrozenForTest resets the frozen flag for testing purposes only.
func resetFactoryFrozenForTest() {
	jobFactoryFrozen.Store(false)
}
