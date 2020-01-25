package cache

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/service/resourcegroupstaggingapi"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/aws/aws-sdk-go/aws/awsutil"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/karlseguin/ccache"
)

type Config struct {
	DefaultTTL     time.Duration
	specificTTL    map[string]time.Duration
	mutatingCaches map[string]bool
	sync.RWMutex
	caches  *sync.Map
	metrics *cacheCollector
	maxSize int64
	itemsToPrune uint32
}

const cacheNameFormat = "%v.%v"

// NewConfig returns a cache configuration with the defaultTTL
func NewConfig(defaultTTL time.Duration, maxSize int64, itemsToPrune uint32) *Config {
	if maxSize == 0 {
		maxSize = 5000
	}
	if itemsToPrune == 0 {
		itemsToPrune = 500
	}
	return &Config{
		DefaultTTL:     defaultTTL,
		specificTTL:    make(map[string]time.Duration),
		mutatingCaches: make(map[string]bool),
		caches:         &sync.Map{},
		maxSize: maxSize,
		itemsToPrune: itemsToPrune,
	}
}

func (c *Config) NewCacheCollector(namespace string) prometheus.Collector {
	c.metrics = newCacheCollector(namespace)
	return c.metrics
}

// SetCacheTTL sets a unique TTL for the service and operation
func (c *Config) SetCacheTTL(serviceName, operationName string, ttl time.Duration) {
	c.specificTTL[fmt.Sprintf(cacheNameFormat, serviceName, operationName)] = ttl
}

// SetCacheMutating sets a specific operation to mutating/non-mutating
func (c *Config) SetCacheMutating(serviceName, operationName string, isMutating bool) {
	c.mutatingCaches[fmt.Sprintf(cacheNameFormat, serviceName, operationName)] = isMutating
}

// FlushCache flushes all caches for a service
func (c *Config) FlushCache(serviceName string) {
	c.caches.Range(func(k, v interface{}) bool {
		cacheName := k.(string)
		if strings.HasPrefix(cacheName, serviceName) {
			c.Lock()
			o, _ := c.caches.Load(cacheName)
			ccacheInstance := o.(*ccache.Cache)
			c.caches.Store(cacheName, ccache.New(ccache.Configure().MaxSize(c.maxSize).ItemsToPrune(c.itemsToPrune)))
			ccacheInstance.Stop()
			c.Unlock()
			n := strings.Split(cacheName, ".")
			c.incFlush(n[0], n[1])
		}
		return true
	})
}

func (c *Config) FlushOperationCache(serviceName, operationName string) {
	c.caches.Range(func(k, v interface{}) bool {
		cacheName := k.(string)
		if cacheName == fmt.Sprintf(cacheNameFormat, serviceName, operationName) {
			c.Lock()
			o, _ := c.caches.Load(cacheName)
			ccacheInstance := o.(*ccache.Cache)
			c.caches.Store(cacheName, ccache.New(ccache.Configure().MaxSize(c.maxSize).ItemsToPrune(c.itemsToPrune)))
			ccacheInstance.Stop()
			c.Unlock()
		}
		return true
	})
}

func (c *Config) flushCaches(r *request.Request) {
	opName := r.Operation.Name
	serviceName := r.ClientInfo.ServiceName

	if isCachable(opName) {
		return
	}

	if c.isMutating(serviceName, opName) {
		c.FlushCache(serviceName)
	} else {
		c.FlushOperationCache(serviceName, opName)
	}

	if strings.Contains(opName, "Tags") {
		c.FlushCache(resourcegroupstaggingapi.ServiceName)
	}
}

func (c *Config) getCache(r *request.Request) *ccache.Cache {
	_, ok := c.caches.Load(cacheName(r))
	if !ok {
		cache := ccache.New(ccache.Configure().MaxSize(c.maxSize).ItemsToPrune(c.itemsToPrune))
		c.caches.Store(cacheName(r), cache)
	}
	o, _ := c.caches.Load(cacheName(r))
	return o.(*ccache.Cache)
}

func (c *Config) get(r *request.Request) *ccache.Item {
	c.RLock()
	defer c.RUnlock()
	return c.getCache(r).Get(cacheKey(r))
}

func (c *Config) set(r *request.Request, object interface{}) {
	c.RLock()
	defer c.RUnlock()

	if !isCachable(r.Operation.Name) {
		return
	}

	// Check for custom ttl
	ttl, ok := c.specificTTL[cacheName(r)]
	if !ok {
		ttl = c.DefaultTTL
	}

	c.getCache(r).Set(cacheKey(r), object, ttl)
}

func (c *Config) isMutating(serviceName, operationName string) bool {
	// assume cache is mutating by default
	if val, ok := c.mutatingCaches[fmt.Sprintf(cacheNameFormat, serviceName, operationName)]; ok {
		return val
	}
	return true
}

func cacheName(r *request.Request) string {
	return fmt.Sprintf(cacheNameFormat, r.ClientInfo.ServiceName, r.Operation.Name)
}

func cacheKey(r *request.Request) string {
	return awsutil.Prettify(r.Params)
}

func isCachable(operationName string) bool {
	if !(strings.HasPrefix(operationName, "Describe") ||
		strings.HasPrefix(operationName, "List") ||
		strings.HasPrefix(operationName, "Get")) {
		return false
	}
	return true
}

func (c *Config) incHit(r *request.Request) {
	if c.metrics != nil {
		c.metrics.incHit(r)
	}
}

func (c *Config) incMiss(r *request.Request) {
	if c.metrics != nil {
		c.metrics.incMiss(r)
	}
}

func (c *Config) incFlush(serviceName, operationName string) {
	if c.metrics != nil {
		c.metrics.incFlush(serviceName, operationName)
	}
}
