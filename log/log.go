package log

type LogEntry struct {
	Index   int
	Term    int
	Command string
}
