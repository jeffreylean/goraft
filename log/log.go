package log

type LogEntry struct {
	// Index of each log. Should be equal to index of logs array
	Index   int
	Term    int
	Command string
}
