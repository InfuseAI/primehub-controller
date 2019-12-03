package graphql

import (
	"encoding/json"
	"fmt"
	v1 "k8s.io/api/core/v1"
	serial "k8s.io/apimachinery/pkg/runtime/serializer/json"
	"os"
	"testing"
)

func TestFetchContext(t *testing.T) {
	t.Skip()

	userId := ""
	graphqlEndpoint := ""
	graphqlSecret := ""

	client := NewGraphqlClient(graphqlEndpoint, graphqlSecret)
	result, err := client.FetchByUserId(userId)
	if err != nil {
		panic(err)
	}
	bytes, _ := json.Marshal(result)
	fmt.Printf("result: %s\n", string(bytes))

	var spawner *Spawner
	pod := v1.Pod{}

	options := SpawnerDataOptions{}
	spawner, err = NewSpawnerByData(result.Data, "phusers", "cpu-only", "base-notebook", options)
	if err != nil {
		panic(err)
	}
	spawner.WithCommand([]string{"echo", "helloworld"}).BuildPodSpec(&pod.Spec)

	serializer := serial.NewSerializerWithOptions(serial.DefaultMetaFactory, nil, nil, serial.SerializerOptions{})
	serializer.Encode(&pod, os.Stdout)
}
