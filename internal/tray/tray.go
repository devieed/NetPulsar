package tray

// Labels are the right-click menu captions.
type Labels struct {
	Settings string
	Close    string
}

// Handlers run on the UI thread of the tray icon.
type Handlers struct {
	Show     func()
	Settings func()
	Close    func()
}
