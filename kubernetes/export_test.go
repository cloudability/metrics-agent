package kubernetes

var (
	IsFargateNode     = isFargateNode
	GetFirstReadyNode = getFirstReadyNode
	EnsureNodeSource  = ensureNodeSource
	DownloadNodeData  = downloadNodeData
	Unreachable       = unreachable
	Proxy             = proxy
	Direct            = direct
)
