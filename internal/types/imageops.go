package types

type ImagePullReq struct {
	Image string `json:"image"`
}

type ImagePruneReq struct {
	Keep     int  `json:"keep"`
	Dangling bool `json:"dangling"`
	DryRun   bool `json:"dryRun"`
}

type SystemPruneReq struct {
	BuildCache     bool `json:"buildCache"`
	DanglingImages bool `json:"danglingImages"`
}
