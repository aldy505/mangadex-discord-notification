package main

import (
	"net/http"
	"net/url"
)

type MangadexClient struct {
	baseURL    *url.URL
	httpClient *http.Client
}

func NewMangadexClient(baseURL string, httpClient *http.Client) (*MangadexClient, error) {
	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		return nil, err
	}

	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	return &MangadexClient{
		baseURL:    parsedURL,
		httpClient: httpClient,
	}, nil
}
