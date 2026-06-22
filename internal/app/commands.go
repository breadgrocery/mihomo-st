package app

type ProxyServerOverride struct {
	Expand      *bool
	Nameservers []string
	Timeout     *int
}

type ProxyImportCommand struct {
	Type           string
	Payload        string
	Mode           string
	Headers        map[string]string
	Timeout        *int
	FollowRedirect *bool
	ProxyServer    *ProxyServerOverride
}

type ProxyRequestCommand struct {
	URL            string
	Method         *string
	Headers        map[string]string
	Timeout        *int
	FollowRedirect *bool
	Body           *string
}

type DelayCommand struct {
	Timeout        *int
	Headers        map[string]string
	FollowRedirect *bool
	Expected       *string
	Rounds         *int
	Unified        *bool
	URLs           []DelayTargetCommand
}

type DelayCollectionCommand struct {
	DelayCommand
	Concurrency *int
}

type DelayTargetCommand struct {
	URL            string
	Timeout        *int
	Headers        map[string]string
	FollowRedirect *bool
	Expected       *string
	Rounds         *int
	Unified        *bool
}

type DownloadCommand struct {
	Timeout        *int
	Headers        map[string]string
	FollowRedirect *bool
	Rounds         *int
	MaxBytes       *int
	URLs           []DownloadTargetCommand
}

type DownloadCollectionCommand struct {
	DownloadCommand
	Concurrency *int
}

type DownloadTargetCommand struct {
	URL            string
	Timeout        *int
	Headers        map[string]string
	FollowRedirect *bool
	Rounds         *int
	MaxBytes       *int
}
