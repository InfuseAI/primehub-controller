package graphql

import (
	v1 "k8s.io/api/core/v1"
	serial "k8s.io/apimachinery/pkg/runtime/serializer/json"
	"os"
	"testing"
)

func TestFetchContext(t *testing.T) {
	userId := "unittest-user-id"

	mockClient := new(MockAbstractGraphqlClient)

	mockResult := &DtoResult{
		Data: DtoData{
			System: DtoSystem{},
			User: DtoUser{
				Id:       userId,
				Username: "Test User",
				IsAdmin:  true,
				Groups: []DtoGroup{
					{
						Name:          "phusers",
						InstanceTypes: []DtoInstanceType{{Name: "cpu-only"}},
						Images:        []DtoImage{{Name: "base-notebook"}},
					},
				},
			},
		},
	}
	mockClient.On("FetchByUserId", userId).Return(mockResult, nil)

	result, err := mockClient.FetchByUserId(userId)
	if err != nil {
		panic(err)
	}

	var spawner *Spawner
	pod := v1.Pod{}

	options := SpawnerOptions{}
	spawner, err = NewSpawnerForJob(result.Data, "phusers", "cpu-only", "base-notebook", options)
	if err != nil {
		panic(err)
	}
	spawner.WithCommand([]string{"echo", "helloworld"}).BuildPodSpec(&pod.Spec)

	serializer := serial.NewSerializerWithOptions(serial.DefaultMetaFactory, nil, nil, serial.SerializerOptions{})
	serializer.Encode(&pod, os.Stdout)
}
