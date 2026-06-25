package gopact

func resetDefaultsForTest(t interface{ Cleanup(func()) }) {
	previous := globalDefaults.Load()
	globalDefaults.Store(nil)
	t.Cleanup(func() {
		globalDefaults.Store(previous)
	})
}
