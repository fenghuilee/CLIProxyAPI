package main

import "strings"

// VolcengineAdapter uses the SDK's standard regional Ark endpoint.
type VolcengineAdapter struct{ *arkSDKClient }

func NewVolcengineAdapter(cfg VolcengineConfig) (*VolcengineAdapter, error) {
	cfg.normalize("volcengine")
	if cfg.Region == "" {
		cfg.Region = "cn-beijing"
	}
	if err := cfg.validate("volcengine"); err != nil {
		return nil, err
	}
	client, err := newArkSDKClient(cfg.ChannelConfig, cfg.Region, "")
	if err != nil {
		return nil, err
	}
	return &VolcengineAdapter{arkSDKClient: client}, nil
}

func normalizeAssetType(fileType string) string {
	switch strings.ToLower(strings.TrimSpace(fileType)) {
	case "video":
		return "Video"
	case "audio":
		return "Audio"
	default:
		return "Image"
	}
}
