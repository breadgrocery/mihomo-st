package server

import (
	"mihomo-st/internal/app"
	"mihomo-st/internal/proxyconfig"
	"mihomo-st/internal/store"
	"mihomo-st/internal/version"
)

type versionResponseDTO struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type digestResponseDTO struct {
	Digest string `json:"digest"`
}

type proxyInfoResponseDTO struct {
	Type   string `json:"type"`
	Name   string `json:"name"`
	Server string `json:"server"`
	Port   any    `json:"port"`
	Digest string `json:"digest"`
}

type proxyListResponseDTO struct {
	Version int                    `json:"version"`
	Proxies []proxyInfoResponseDTO `json:"proxies"`
}

type importProxiesResponseDTO struct {
	Version  int                    `json:"version"`
	Proxies  []proxyInfoResponseDTO `json:"proxies"`
	Warnings []warningResponseDTO   `json:"warnings"`
}

type warningResponseDTO struct {
	Index   int    `json:"index"`
	Message string `json:"message"`
}

type delayResponseDTO struct {
	Version int    `json:"version"`
	Digest  string `json:"digest"`
	Min     int    `json:"delay-min"`
	Max     int    `json:"delay-max"`
	Avg     int    `json:"delay-avg"`
	Cost    int    `json:"delay-cost"`
	Success int    `json:"success"`
	Failed  int    `json:"failed"`
	Total   int    `json:"total"`
	Error   string `json:"error,omitempty"`
}

type delayCollectionResponseDTO struct {
	Version int                      `json:"version"`
	Results []delayResultResponseDTO `json:"results"`
}

type delayResultResponseDTO struct {
	Digest  string `json:"digest"`
	Min     int    `json:"delay-min"`
	Max     int    `json:"delay-max"`
	Avg     int    `json:"delay-avg"`
	Cost    int    `json:"delay-cost"`
	Success int    `json:"success"`
	Failed  int    `json:"failed"`
	Total   int    `json:"total"`
	Error   string `json:"error,omitempty"`
}

type downloadResponseDTO struct {
	Version int    `json:"version"`
	Digest  string `json:"digest"`
	Min     int    `json:"speed-min"`
	Max     int    `json:"speed-max"`
	Avg     int    `json:"speed-avg"`
	Score   int    `json:"speed-score"`
	Success int    `json:"success"`
	Failed  int    `json:"failed"`
	Total   int    `json:"total"`
	Error   string `json:"error,omitempty"`
}

type downloadCollectionResponseDTO struct {
	Version int                         `json:"version"`
	Results []downloadResultResponseDTO `json:"results"`
}

type downloadResultResponseDTO struct {
	Digest  string `json:"digest"`
	Min     int    `json:"speed-min"`
	Max     int    `json:"speed-max"`
	Avg     int    `json:"speed-avg"`
	Score   int    `json:"speed-score"`
	Success int    `json:"success"`
	Failed  int    `json:"failed"`
	Total   int    `json:"total"`
	Error   string `json:"error,omitempty"`
}

func versionResponse() versionResponseDTO {
	return versionResponseDTO{
		Name:    version.Name,
		Version: version.Version,
	}
}

func proxyListResponseFromResult(result app.ProxyListResult) proxyListResponseDTO {
	return proxyListResponseDTO{
		Version: result.Version,
		Proxies: proxyInfoResponses(result.Proxies),
	}
}

func importProxiesResponseFromResult(result app.ProxyImportResult) importProxiesResponseDTO {
	return importProxiesResponseDTO{
		Version:  result.Version,
		Proxies:  proxyInfoResponses(result.Proxies),
		Warnings: warningResponses(result.Warnings),
	}
}

func proxyInfoResponses(proxies []store.ProxyInfo) []proxyInfoResponseDTO {
	responses := make([]proxyInfoResponseDTO, len(proxies))
	for idx, proxy := range proxies {
		responses[idx] = proxyInfoResponseDTO{
			Type:   proxy.Type,
			Name:   proxy.Name,
			Server: proxy.Server,
			Port:   proxy.Port,
			Digest: proxy.Digest,
		}
	}
	return responses
}

func warningResponses(warnings []proxyconfig.Warning) []warningResponseDTO {
	responses := make([]warningResponseDTO, len(warnings))
	for idx, warning := range warnings {
		responses[idx] = warningResponseDTO{
			Index:   warning.Index,
			Message: warning.Message,
		}
	}
	return responses
}

func delayResponseFromResult(result app.DelayResult) delayResponseDTO {
	fields := delayResultResponseFromResult(result)
	return delayResponseDTO{
		Version: result.Version,
		Digest:  fields.Digest,
		Min:     fields.Min,
		Max:     fields.Max,
		Avg:     fields.Avg,
		Cost:    fields.Cost,
		Success: fields.Success,
		Failed:  fields.Failed,
		Total:   fields.Total,
		Error:   fields.Error,
	}
}

func delayCollectionResponseFromResult(result app.DelayCollectionResult) delayCollectionResponseDTO {
	response := delayCollectionResponseDTO{
		Version: result.Version,
		Results: make([]delayResultResponseDTO, len(result.Results)),
	}
	for idx, item := range result.Results {
		response.Results[idx] = delayResultResponseFromResult(item)
	}
	return response
}

func delayResultResponseFromResult(result app.DelayResult) delayResultResponseDTO {
	return delayResultResponseDTO{
		Digest:  result.Digest,
		Min:     result.Metrics.Min,
		Max:     result.Metrics.Max,
		Avg:     result.Metrics.Avg,
		Cost:    result.Metrics.Cost,
		Success: result.Metrics.Success,
		Failed:  result.Metrics.Failed,
		Total:   result.Metrics.Total,
		Error:   result.Metrics.Error,
	}
}

func downloadResponseFromResult(result app.DownloadResult) downloadResponseDTO {
	fields := downloadResultResponseFromResult(result)
	return downloadResponseDTO{
		Version: result.Version,
		Digest:  fields.Digest,
		Min:     fields.Min,
		Max:     fields.Max,
		Avg:     fields.Avg,
		Score:   fields.Score,
		Success: fields.Success,
		Failed:  fields.Failed,
		Total:   fields.Total,
		Error:   fields.Error,
	}
}

func downloadCollectionResponseFromResult(result app.DownloadCollectionResult) downloadCollectionResponseDTO {
	response := downloadCollectionResponseDTO{
		Version: result.Version,
		Results: make([]downloadResultResponseDTO, len(result.Results)),
	}
	for idx, item := range result.Results {
		response.Results[idx] = downloadResultResponseFromResult(item)
	}
	return response
}

func downloadResultResponseFromResult(result app.DownloadResult) downloadResultResponseDTO {
	return downloadResultResponseDTO{
		Digest:  result.Digest,
		Min:     result.Metrics.Min,
		Max:     result.Metrics.Max,
		Avg:     result.Metrics.Avg,
		Score:   result.Metrics.Score,
		Success: result.Metrics.Success,
		Failed:  result.Metrics.Failed,
		Total:   result.Metrics.Total,
		Error:   result.Metrics.Error,
	}
}
