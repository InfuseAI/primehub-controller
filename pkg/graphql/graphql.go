package graphql

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

type Location struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

type QueryError struct {
	Message       string                 `json:"message"`
	Locations     []Location             `json:"locations,omitempty"`
	Path          []interface{}          `json:"path,omitempty"`
	Rule          string                 `json:"-"`
	ResolverError error                  `json:"-"`
	Extensions    map[string]interface{} `json:"extensions,omitempty"`
}

type QueryErrors struct {
	Errors []QueryError `json:"errors"`
}

// Type definitions of data transfer objects (DTO) from graphql
type DtoResult struct {
	Data DtoData
}

type DtoData struct {
	System DtoSystem
	User   DtoUser
}

type DtoSystem struct {
	DefaultUserVolumeCapacity int
}

type DtoUser struct {
	Id             string
	Username       string
	IsAdmin        bool
	VolumeCapacity string
	Email          string
	Groups         []DtoGroup
}

type DtoGroup struct {
	Name                            string
	DisplayName                     string
	EnabledSharedVolume             bool
	SharedVolumeCapacity            string
	HomeSymlink                     *bool
	LaunchGroupOnly                 *bool
	QuotaCpu                        float32
	QuotaGpu                        float32
	QuotaMemory                     string
	UserVolumeCapacity              string
	ProjectQuotaCpu                 float32
	ProjectQuotaGpu                 float32
	ProjectQuotaMemory              string
	EnabledDeployment               bool
	JobDefaultActiveDeadlineSeconds *int64

	InstanceTypes []DtoInstanceType
	Images        []DtoImage
	Datasets      []DtoDataset

	Mlflow *DtoMlflow
}

type DtoMlflow struct {
	TrackingUri  string
	UiUrl        string
	TrackingEnvs []corev1.EnvVar
	ArtifactEnvs []corev1.EnvVar
}

type DtoInstanceType struct {
	Name        string
	Description string
	DisplayName string
	Global      bool
	Spec        DtoInstanceTypeSpec
}

// https://gitlab.com/infuseai/canner-admin-ui/blob/master/packages/graphql-server/src/graphql/instanceType.graphql
type DtoInstanceTypeSpec struct {
	LimitsCpu       float32 `json:"limits.cpu"`
	RequestsCpu     float32 `json:"requests.cpu"`
	RequestsMemory  string  `json:"requests.memory"`
	LimitsMemory    string  `json:"limits.memory"`
	RequestsGpu     int     `json:"requests.gpu"`
	LimitsGpu       int     `json:"limits.gpu"`
	GpuResourceName string  `json:"gpuResourceName"`
	NodeSelector    map[string]string
	Tolerations     []corev1.Toleration
}

type DtoImage struct {
	Name        string
	Description string
	DisplayName string
	Global      bool
	Spec        DtoImageSpec
}

type DtoImageSpec struct {
	Name string

	Type       string
	Url        string
	UrlForGpu  string
	PullSecret string
}

type DtoDataset struct {
	Name            string
	DisplayName     string
	Description     string
	Global          bool
	Writable        bool
	MountRoot       string
	HomeSymlink     *bool
	LaunchGroupOnly *bool
	Spec            DtoDatasetSpec
}

type DtoDatasetSpec struct {
	EnableUploadServer bool
	Type               string
	Url                string
	VolumeName         string
	Variables          map[string]string
	GitSyncHostRoot    string
	GitSyncRoot        string
	HostPath           DtoDatasetSpecHostPath
	Nfs                DtoDatasetSpecNfs
	Pv                 DtoDatasetSpecPv
}

type DtoDatasetSpecHostPath struct {
	Path string
}

type DtoDatasetSpecNfs struct {
	Server string
	Path   string
}

type DtoDatasetSpecPv struct {
	Provisioning string
}

type AbstractGraphqlClient interface {
	FetchByUserId(string) (*DtoResult, error)
	FetchEmailByUserId(string) (string, error)
	QueryServer(map[string]interface{}) ([]byte, error)
	FetchGroupEnableModelDeployment(string) (bool, error)
	FetchGroupInfo(string) (*DtoGroup, error)
	FetchGroupInfoByName(string) (*DtoGroup, error)
	FetchGlobalDatasets() ([]DtoDataset, error)
	FetchInstanceTypeInfo(string) (*DtoInstanceType, error)
	FetchTimeZone() (string, error)
	NotifyPhJobEvent(id string, eventType string) (float64, error)
}

type GraphqlClient struct {
	graphqlEndpoint string
	graphqlSecret   string
}

func NewGraphqlClient(graphqlEndpoint string, graphqlSecret string) AbstractGraphqlClient {
	return &GraphqlClient{graphqlEndpoint: graphqlEndpoint, graphqlSecret: graphqlSecret}
}

func (c GraphqlClient) FetchByUserId(userId string) (*DtoResult, error) {
	query := `
	query ($id: ID!) {
		system { defaultUserVolumeCapacity }
		user (where: { id: $id  }) { id username isAdmin volumeCapacity
			groups { name
					displayName
					enabledSharedVolume
					sharedVolumeCapacity
					homeSymlink
					launchGroupOnly
					quotaCpu
					quotaGpu
					quotaMemory
					userVolumeCapacity
					projectQuotaCpu
					projectQuotaGpu
					projectQuotaMemory
					instanceTypes { name displayName description spec global }
					images { name displayName description spec global }
					datasets { name displayName description spec global writable mountRoot homeSymlink launchGroupOnly }
					mlflow { trackingUri uiUrl trackingEnvs { name value } artifactEnvs { name value } }
			}
		}
	}
	`

	requestData := map[string]interface{}{
		"query": query,
		"variables": map[string]interface{}{
			"id": userId,
		},
	}

	requestJson, _ := json.Marshal(requestData)

	request, err := http.NewRequest(http.MethodPost, c.graphqlEndpoint, strings.NewReader(string(requestJson)))
	if err != nil {
		return nil, err
	}

	request.Header.Add("Content-Type", "application/json")
	request.Header.Add("Authorization", "Bearer "+c.graphqlSecret)
	client := &http.Client{}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}

	if response.StatusCode != 200 {
		return nil, errors.New("graphql query failed: " + response.Status)
	}

	defer response.Body.Close()
	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	//fmt.Println(string(body))
	var result DtoResult
	json.Unmarshal(body, &result)

	if result.Data.User.Id == "" {
		return nil, errors.New("User not found")
	}

	return &result, nil
}

func (c GraphqlClient) FetchEmailByUserId(userId string) (string, error) {
	query := `
	query ($id: ID!) {
		user (where: { id: $id  }) { id username email }
	}
	`

	requestData := map[string]interface{}{
		"query": query,
		"variables": map[string]interface{}{
			"id": userId,
		},
	}

	requestJson, _ := json.Marshal(requestData)

	request, err := http.NewRequest(http.MethodPost, c.graphqlEndpoint, strings.NewReader(string(requestJson)))
	if err != nil {
		return "", err
	}

	request.Header.Add("Content-Type", "application/json")
	request.Header.Add("Authorization", "Bearer "+c.graphqlSecret)
	client := &http.Client{}
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}

	if response.StatusCode != 200 {
		return "", errors.New("graphql query failed: " + response.Status)
	}

	defer response.Body.Close()
	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return "", err
	}
	var result DtoResult
	json.Unmarshal(body, &result)

	if result.Data.User.Id == "" {
		return "", errors.New("User not found")
	}

	return result.Data.User.Email, nil
}

func (c GraphqlClient) QueryServer(requestData map[string]interface{}) ([]byte, error) {
	requestJson, _ := json.Marshal(requestData)

	request, err := http.NewRequest(http.MethodPost, c.graphqlEndpoint, strings.NewReader(string(requestJson)))
	if err != nil {
		return nil, err
	}

	request.Header.Add("Content-Type", "application/json")
	request.Header.Add("Authorization", "Bearer "+c.graphqlSecret)
	client := &http.Client{}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}

	if response.StatusCode != 200 {
		body, _ := ioutil.ReadAll(response.Body)
		return body, errors.New("graphql query failed: " + response.Status)
	}

	defer response.Body.Close()
	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	return body, nil
}

func processGraphQLErrorMessage(body []byte) string {
	data := &QueryErrors{}
	if body == nil {
		return ""
	}
	json.Unmarshal(body, &data)
	return data.Errors[0].Message
}

func (c GraphqlClient) FetchGroupEnableModelDeployment(groupId string) (bool, error) {
	query := `
	query ($id: ID!) {
		group(where: {id: $id}) {
					name
					id
					enabledDeployment
	  }
	}
	`
	requestData := map[string]interface{}{
		"query": query,
		"variables": map[string]interface{}{
			"id": groupId,
		},
	}
	body, err := c.QueryServer(requestData)
	if err != nil {
		return false, errors.New(processGraphQLErrorMessage(body))
	}
	data := map[string]interface{}{}
	json.Unmarshal(body, &data)

	data, ok := data["data"].(map[string]interface{})
	if !ok {
		return false, errors.New("can not find data in response")
	}
	_group, ok := data["group"].(map[string]interface{})
	if !ok {
		return false, errors.New("can not find group in response")
	}

	var group DtoGroup
	jsonObj, _ := json.Marshal(_group)
	json.Unmarshal(jsonObj, &group)

	if _group["enabledDeployment"] == nil {
		group.EnabledDeployment = false
	}

	return group.EnabledDeployment, nil
}

func (c GraphqlClient) FetchGroupInfoByName(groupName string) (*DtoGroup, error) {
	query := `
	query ($name: String!) {
		groups(where: {name_contains: $name}) { 
			id
			name
			quotaCpu
			quotaGpu
			quotaMemory
			projectQuotaCpu
			projectQuotaGpu
			projectQuotaMemory
			jobDefaultActiveDeadlineSeconds
			enabledSharedVolume
			sharedVolumeCapacity
			homeSymlink
			launchGroupOnly
			datasets { name displayName description spec global writable mountRoot homeSymlink launchGroupOnly }
	  }
	}
	`
	requestData := map[string]interface{}{
		"query": query,
		"variables": map[string]interface{}{
			"name": groupName,
		},
	}
	body, err := c.QueryServer(requestData)
	if err != nil {
		return nil, err
	}
	data := map[string]interface{}{}
	json.Unmarshal(body, &data)

	data, ok := data["data"].(map[string]interface{})
	if !ok {
		return nil, errors.New("can not find data in response")
	}
	_groups, ok := data["groups"].([]interface{})
	if !ok {
		return nil, errors.New("can not find groups in response")
	}

	var group DtoGroup
	var _group map[string]interface{}
	ok = false
	// Match group name
	for _, g := range _groups {
		if groupName == g.(map[string]interface{})["name"].(string) {
			_group = g.(map[string]interface{})
			ok = true
			break
		}
	}
	if !ok {
		return nil, errors.New("can not find group (" + groupName + ") in response")
	}

	jsonObj, _ := json.Marshal(_group)
	json.Unmarshal(jsonObj, &group)

	if _group["quotaCpu"] == nil {
		group.QuotaCpu = -1
	}
	if _group["quotaGpu"] == nil {
		group.QuotaGpu = -1
	}
	if _group["projectQuotaCpu"] == nil {
		group.ProjectQuotaCpu = -1
	}
	if _group["projectQuotaGpu"] == nil {
		group.ProjectQuotaGpu = -1
	}

	return &group, nil
}

func (c GraphqlClient) FetchGlobalDatasets() ([]DtoDataset, error) {
	query := `
	query {
		datasets { 
			name displayName description spec global writable mountRoot homeSymlink launchGroupOnly
	  }
	}`
	requestData := map[string]interface{}{
		"query": query,
	}
	body, err := c.QueryServer(requestData)
	if err != nil {
		return nil, err
	}
	data := map[string]interface{}{}
	json.Unmarshal(body, &data)

	data, ok := data["data"].(map[string]interface{})
	if !ok {
		return nil, errors.New("can not find data in response")
	}
	_datasets, ok := data["datasets"].([]interface{})
	if !ok {
		return nil, errors.New("can not find datasets in response")
	}

	var globalDatasets []DtoDataset

	for _, d := range _datasets {
		var dataset DtoDataset
		jsonObj, _ := json.Marshal(d)
		json.Unmarshal(jsonObj, &dataset)
		if dataset.Global == true {
			globalDatasets = append(globalDatasets, dataset)
		}
	}

	return globalDatasets, nil
}

func (c GraphqlClient) FetchGroupInfo(groupId string) (*DtoGroup, error) {
	query := `
	query ($id: ID!) {
		group(where: {id: $id}) { 
					name
					id
					quotaCpu
					quotaGpu
					quotaMemory
					projectQuotaCpu
					projectQuotaGpu
					projectQuotaMemory
					jobDefaultActiveDeadlineSeconds
	  }
	}
	`
	requestData := map[string]interface{}{
		"query": query,
		"variables": map[string]interface{}{
			"id": groupId,
		},
	}
	body, err := c.QueryServer(requestData)
	if err != nil {
		return nil, err
	}
	data := map[string]interface{}{}
	json.Unmarshal(body, &data)

	data, ok := data["data"].(map[string]interface{})
	if !ok {
		return nil, errors.New("can not find data in response")
	}
	_group, ok := data["group"].(map[string]interface{})
	if !ok {
		return nil, errors.New("can not find group in response")
	}

	var group DtoGroup
	jsonObj, _ := json.Marshal(_group)
	json.Unmarshal(jsonObj, &group)

	if _group["quotaCpu"] == nil {
		group.QuotaCpu = -1
	}
	if _group["quotaGpu"] == nil {
		group.QuotaGpu = -1
	}
	if _group["projectQuotaCpu"] == nil {
		group.ProjectQuotaCpu = -1
	}
	if _group["projectQuotaGpu"] == nil {
		group.ProjectQuotaGpu = -1
	}

	return &group, nil
}

func (c GraphqlClient) FetchInstanceTypeInfo(instanceTypeId string) (*DtoInstanceType, error) {
	query := `
	query ($id: ID!) {
		instanceType(where: {id: $id}) { 
					name
					id
					description
					spec
					global
	  }
	}
	`
	requestData := map[string]interface{}{
		"query": query,
		"variables": map[string]interface{}{
			"id": instanceTypeId,
		},
	}
	body, err := c.QueryServer(requestData)
	if err != nil {
		return nil, err
	}
	data := map[string]interface{}{}
	json.Unmarshal(body, &data)

	data, ok := data["data"].(map[string]interface{})
	if !ok {
		return nil, errors.New("can not find data in response")
	}
	_instanceType, ok := data["instanceType"].(map[string]interface{})
	if !ok {
		return nil, errors.New("can not find instanceType in response")
	}
	_spec, ok := _instanceType["spec"].(map[string]interface{})
	if !ok {
		return nil, errors.New("can not find instanceType.spec in response")
	}

	var instanceType DtoInstanceType
	jsonObj, _ := json.Marshal(_instanceType)
	json.Unmarshal(jsonObj, &instanceType)
	if _spec["requests.cpu"] == nil {
		instanceType.Spec.RequestsCpu = -1
	}
	return &instanceType, nil
}

// FetchTimeZone get timezone from system
func (c GraphqlClient) FetchTimeZone() (string, error) {

	query := `
	query {
		system {
			timezone {
			name
			offset
			}
		}
	}
	`
	requestData := map[string]interface{}{
		"query": query,
	}

	body, err := c.QueryServer(requestData)
	if err != nil {
		return "", err
	}
	data := map[string]interface{}{}
	json.Unmarshal(body, &data)

	data, ok := data["data"].(map[string]interface{})
	if !ok {
		return "", errors.New("can not find data in response")
	}

	_system, ok := data["system"].(map[string]interface{})
	if !ok {
		return "", errors.New("can not find system in response")
	}

	_timezone, ok := _system["timezone"].(map[string]interface{})
	if !ok {
		return "", errors.New("can not find system.timezone in response")
	}

	if _timezone["name"] == nil {
		return "", errors.New("there is no system.timezone.name in response")
	}

	return _timezone["name"].(string), nil
}

// NotifyPhJobEvent to send to the graphql
func (c GraphqlClient) NotifyPhJobEvent(id string, eventType string) (float64, error) {

	query := `
	mutation ($data: PhJobNotifyEventInput!) {
		notifyPhJobEvent (data: $data)
	}
	`
	requestData := map[string]interface{}{
		"query": query,
		"variables": map[string]interface{}{
			"data": map[string]interface{}{
				"id":   id,
				"type": eventType,
			},
		},
	}

	body, err := c.QueryServer(requestData)
	if err != nil {
		return 1, err
	}
	data := map[string]interface{}{}
	json.Unmarshal(body, &data)

	data, ok := data["data"].(map[string]interface{})
	if !ok {
		return 1, errors.New("can not find data in response")
	}

	if data["notifyPhJobEvent"] == nil {
		return 1, errors.New("there is no notifyPhJobEvent in response")
	}

	return data["notifyPhJobEvent"].(float64), nil
}

func BuildMlflowEnvironmentVariables(groupName string, result *DtoResult) []corev1.EnvVar {
	var envs []corev1.EnvVar
	for _, group := range result.Data.User.Groups {
		if group.Name == groupName {
			if group.Mlflow != nil {
				envs = append(envs, corev1.EnvVar{Name: "MLFLOW_TRACKING_URI", Value: group.Mlflow.TrackingUri})
				envs = append(envs, corev1.EnvVar{Name: "MLFLOW_UI_URL", Value: group.Mlflow.UiUrl})
				for _, v := range group.Mlflow.TrackingEnvs {
					envs = append(envs, v)
				}
				for _, v := range group.Mlflow.ArtifactEnvs {
					envs = append(envs, v)
				}
			}
		}
	}
	return envs
}
