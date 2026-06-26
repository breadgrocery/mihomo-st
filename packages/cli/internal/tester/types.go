package tester

type Tester struct{}

type DelayPlan struct {
	Targets []DelayTarget
}

type DelayTarget struct {
	URL            string
	Timeout        int
	Headers        map[string]string
	FollowRedirect bool
	Expected       string
	Rounds         int
	Unified        bool
}

type DelayMetrics struct {
	Min     int
	Max     int
	Avg     int
	Cost    int
	Success int
	Failed  int
	Total   int
	Error   string
}

type DelayResult struct {
	Digest  string
	Metrics DelayMetrics
}

type DownloadPlan struct {
	Targets []DownloadTarget
}

type DownloadTarget struct {
	URL            string
	Timeout        int
	Headers        map[string]string
	FollowRedirect bool
	Rounds         int
	MaxBytes       int
}

type DownloadMetrics struct {
	Min     int
	Max     int
	Avg     int
	Score   int
	Success int
	Failed  int
	Total   int
	Error   string
}

type DownloadResult struct {
	Digest  string
	Metrics DownloadMetrics
}
