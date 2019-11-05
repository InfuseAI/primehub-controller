package graphql

import (
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
	fmt.Printf("result: %v\n", result)

	var spawner *Spawner
	pod := v1.Pod{}

	spawner, err = NewSpawnerByData(result.Data, "phusers", "cpu-only", "base-notebook")
	if err != nil {
		panic(err)
	}
	spawner.WithCommand([]string{"echo", "helloworld"}).BuildPodSpec(&pod.Spec)

	serializer := serial.NewSerializerWithOptions(serial.DefaultMetaFactory, nil, nil, serial.SerializerOptions{})
	serializer.Encode(&pod, os.Stdout)
}
