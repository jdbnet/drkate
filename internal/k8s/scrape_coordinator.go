package k8s

import (
	"context"
	"errors"
	"sync"
)

var ErrScrapeInProgress = errors.New("scrape in progress")

type ScrapeCoordinator struct {
	mu         sync.Mutex
	scraping   bool
	lastResult *ScrapeResult
}

func NewScrapeCoordinator() *ScrapeCoordinator {
	return &ScrapeCoordinator{}
}

func (c *ScrapeCoordinator) Run(ctx context.Context, scraper *Scraper) (*ScrapeResult, error) {
	c.mu.Lock()
	if c.scraping {
		c.mu.Unlock()
		return nil, ErrScrapeInProgress
	}
	c.scraping = true
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		c.scraping = false
		c.mu.Unlock()
	}()

	result, err := scraper.Run(ctx)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.lastResult = result
	c.mu.Unlock()
	return result, nil
}

func (c *ScrapeCoordinator) IsScraping() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.scraping
}

func (c *ScrapeCoordinator) LastResult() *ScrapeResult {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lastResult == nil {
		return nil
	}
	copy := *c.lastResult
	return &copy
}
