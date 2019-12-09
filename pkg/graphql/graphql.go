package graphql

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	corev1 "k8s.io/api/core/v1"
	"net/http"
	"strings"
)

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
	Groups         []DtoGroup
}

type DtoGroup struct {
	Name                 string
	DisplayName          string
	EnabledSharedVolume  bool
	SharedVolumeCapacity string
	HomeSymlink          *bool
	LaunchGroupOnly      *bool
	QuotaCpu             float32
	QuotaGpu             float32
	QuotaMemory          string
	UserVolumeCapacity   string
	ProjectQuotaCpu      float32
	ProjectQuotaGpu      float32
	ProjectQuotaMemory   string

	InstanceTypes []DtoInstanceType
	Images        []DtoImage
	Datasets      []DtoDataset
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
	LimitsCpu      float32 `json:"limits.cpu"`
	RequestsCpu    float32 `json:"requests.cpu"`
	RequestsMemory string  `json:"requests.memory"`
	LimitsMemory   string  `json:"limits.memory"`
	RequestsGpu    int     `json:"requests.nvidia.com/gpu"`
	LimitsGpu      int     `json:"limits.nvidia.com/gpu"`
	NodeSelector   map[string]string
	Tolerations    []corev1.Toleration
}

type DtoImage struct {
	Name        string
	Description string
	DisplayName string
	Global      bool
	Spec        DtoImageSpec
}

type DtoImageSpec struct {
	Name 		string

	Type      	string
	Url       	string
	UrlForGpu 	string
	PullSecret	string
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
	Spec			DtoDatasetSpec
}

type DtoDatasetSpec struct {
	EnableUploadServer bool
	Type               string
	Url                string
	VolumeName         string
	Variables          map[string]string
	GitSyncHostRoot	   string
	GitSyncRoot		   string
}

type GraphqlClient struct {
	graphqlEndpoint string
	graphqlSecret   string
}

func NewGraphqlClient(graphqlEndpoint string, graphqlSecret string) *GraphqlClient {
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
