package mailkit

import (
	"errors"
	"math/rand"
	"sync"
	"time"
)

type Config struct {
	Strategy  string
	Providers []Provider
}

type Router struct {
	mutex         sync.Mutex
	strategy      string
	providerNames []string
	providers     map[string]Provider
	failures      map[string]int
	nextIndex     int
	randomSource  *rand.Rand
}

func NewRouter(config Config) (*Router, error) {
	if len(config.Providers) == 0 {
		return nil, errors.New("at least one provider is required")
	}

	strategy := config.Strategy
	if strategy == "" {
		strategy = "round_robin"
	}

	router := &Router{
		strategy:      strategy,
		providerNames: make([]string, 0, len(config.Providers)),
		providers:     make(map[string]Provider, len(config.Providers)),
		failures:      make(map[string]int, len(config.Providers)),
		randomSource:  rand.New(rand.NewSource(time.Now().UnixNano())),
	}

	for _, provider := range config.Providers {
		if provider == nil {
			continue
		}
		providerName := provider.Name()
		if providerName == "" {
			return nil, errors.New("provider name is required")
		}
		if _, exists := router.providers[providerName]; exists {
			return nil, errors.New("duplicate provider name: " + providerName)
		}
		router.providerNames = append(router.providerNames, providerName)
		router.providers[providerName] = provider
		router.failures[providerName] = 0
	}

	if len(router.providerNames) == 0 {
		return nil, errors.New("at least one valid provider is required")
	}

	return router, nil
}

func (router *Router) NextProvider() (Provider, error) {
	router.mutex.Lock()
	defer router.mutex.Unlock()

	if len(router.providerNames) == 0 {
		return nil, errors.New("no providers configured")
	}

	selectedName := router.providerNames[0]
	switch router.strategy {
	case "random":
		selectedName = router.providerNames[router.randomSource.Intn(len(router.providerNames))]
	case "failover":
		selectedName = router.providerNames[0]
		selectedFailures := router.failures[selectedName]
		for _, providerName := range router.providerNames[1:] {
			if router.failures[providerName] < selectedFailures {
				selectedName = providerName
				selectedFailures = router.failures[providerName]
			}
		}
	default:
		selectedName = router.providerNames[router.nextIndex%len(router.providerNames)]
		router.nextIndex++
	}

	return router.providers[selectedName], nil
}

func (router *Router) ReportSuccess(providerName string) {
	router.mutex.Lock()
	defer router.mutex.Unlock()

	if _, ok := router.failures[providerName]; !ok {
		return
	}
	if router.failures[providerName] > 0 {
		router.failures[providerName]--
	}
}

func (router *Router) ReportFailure(providerName string, _ error) {
	router.mutex.Lock()
	defer router.mutex.Unlock()

	if _, ok := router.failures[providerName]; !ok {
		return
	}
	router.failures[providerName]++
}
