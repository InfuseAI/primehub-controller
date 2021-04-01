package phcache

import (
	"github.com/karlseguin/ccache"
	"primehub-controller/pkg/graphql"
	"time"
)

var (
	instance *PrimeHubCache
)

type PrimeHubCache struct {
	GraphqlClient graphql.AbstractGraphqlClient
	Group         *ccache.Cache
	InstanceType  *ccache.Cache
	Datasets      *ccache.Cache
	TimeZone      *ccache.Cache
	ExpiredTime   time.Duration
}

func NewPrimeHubCache(graphqlClient graphql.AbstractGraphqlClient) *PrimeHubCache {
	if instance == nil {
		instance = &PrimeHubCache{
			GraphqlClient: graphqlClient,
			Group:         ccache.New(ccache.Configure().MaxSize(1000).ItemsToPrune(100)),
			InstanceType:  ccache.New(ccache.Configure().MaxSize(1000).ItemsToPrune(100)),
			Datasets:      ccache.New(ccache.Configure().MaxSize(1000).ItemsToPrune(100)),
			TimeZone:      ccache.New(ccache.Configure().MaxSize(1).ItemsToPrune(1)),
			ExpiredTime:   time.Minute,
		}
	}
	return instance
}

func (r *PrimeHubCache) FetchGroup(groupID string) (*graphql.DtoGroup, error) {
	cacheKey := "group:" + groupID
	cacheItem := r.Group.Get(cacheKey)
	if cacheItem == nil || cacheItem.Expired() {
		groupInfo, err := r.GraphqlClient.FetchGroupInfo(groupID)
		if err != nil {
			return nil, err
		}
		r.Group.Set(cacheKey, groupInfo, r.ExpiredTime)
	}
	return r.Group.Get(cacheKey).Value().(*graphql.DtoGroup), nil
}

func (r *PrimeHubCache) FetchGroupByName(name string) (*graphql.DtoGroup, error) {
	cacheKey := "group:" + name
	cacheItem := r.Group.Get(cacheKey)
	if cacheItem == nil || cacheItem.Expired() {
		groupInfo, err := r.GraphqlClient.FetchGroupInfoByName(name)
		if err != nil {
			return nil, err
		}
		r.Group.Set(cacheKey, groupInfo, r.ExpiredTime)
	}
	return r.Group.Get(cacheKey).Value().(*graphql.DtoGroup), nil
}

func (r *PrimeHubCache) FetchInstanceType(instanceTypeID string) (*graphql.DtoInstanceType, error) {
	cacheKey := "instanceType:" + instanceTypeID
	cacheItem := r.InstanceType.Get(cacheKey)
	if cacheItem == nil || cacheItem.Expired() {
		instanceTypeInfo, err := r.GraphqlClient.FetchInstanceTypeInfo(instanceTypeID)
		if err != nil {
			return nil, err
		}
		r.InstanceType.Set(cacheKey, instanceTypeInfo, r.ExpiredTime)
	}
	return r.InstanceType.Get(cacheKey).Value().(*graphql.DtoInstanceType), nil
}

func (r *PrimeHubCache) FetchGlobalDatasets() ([]graphql.DtoDataset, error) {
	cacheKey := "globalDatasets"
	cacheItem := r.Datasets.Get(cacheKey)
	if cacheItem == nil || cacheItem.Expired() {
		globalDatasets, err := r.GraphqlClient.FetchGlobalDatasets()
		if err != nil {
			return nil, err
		}
		r.Datasets.Set(cacheKey, globalDatasets, r.ExpiredTime)
	}
	return r.Datasets.Get(cacheKey).Value().([]graphql.DtoDataset), nil
}

func (r *PrimeHubCache) FetchTimeZone() (timezone string, err error) {
	cacheKey := "timezone"
	cacheItem := r.TimeZone.Get(cacheKey)
	if cacheItem == nil || cacheItem.Expired() {
		location, err := r.GraphqlClient.FetchTimeZone()
		if err != nil {
			return "", err
		}
		r.TimeZone.Set(cacheKey, location, r.ExpiredTime)
	}
	return r.TimeZone.Get(cacheKey).Value().(string), nil
}
