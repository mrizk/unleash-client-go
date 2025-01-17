package unleash_test

import (
	"io/ioutil"
	"os"
	"testing"
	"time"

	"github.com/Unleash/unleash-client-go/v4"
	"github.com/h2non/gock"
	"github.com/stretchr/testify/assert"
)

func Test_withVariants(t *testing.T) {
	a := assert.New(t)
	demoReader, err := os.Open("demo_app_toggles.json")
	if err != nil {
		t.Fail()
	}
	defer gock.OffAll()
	defer demoReader.Close()

	gock.New("http://foo.com").
		Post("/client/register").
		Reply(200)

	jsonStr, err := read_demo_app_toggles()
	if err != nil {
		t.Fail()
	}

	// Use the string as the body of the Gock request
	gock.New("http://foo.com").
		Get("/client/features").Reply(200).BodyString(jsonStr)
	if err != nil {
		t.Fail()
	}
	err = unleash.Initialize(
		unleash.WithListener(&unleash.DebugListener{}),
		unleash.WithAppName("my-application"),
		unleash.WithRefreshInterval(5*time.Second),
		unleash.WithDisableMetrics(true),
		unleash.WithStorage(&unleash.BootstrapStorage{Reader: demoReader}),
		unleash.WithUrl("http://foo.com"),
	)

	if err != nil {
		t.Fail()
	}

	variant := unleash.GetVariant("Demo")
	if variant.Enabled == false {
		t.Fatalf("Expected variant to be enabled")
	}
	if variant.FeatureEnabled == false {
		t.Fatalf("Expected feature to be enabled")
	}
	if variant.Name != "small" && variant.Name != "medium" {
		t.Fatalf("Expected one of the variant names")
	}
	if variant.Payload.Value != "35" && variant.Payload.Value != "55" {
		t.Fatalf("Expected one of the variant payloads")
	}
	err = unleash.Close()
	a.Nil(err)
}

func read_demo_app_toggles() (string, error) {
	demoReader, err := os.Open("demo_app_toggles.json")
	if err != nil {
		return "", err
	}
	defer demoReader.Close()
	byteValue, _ := ioutil.ReadAll(demoReader)
	return string(byteValue), nil
}

func Test_withVariantsAndANonExistingStrategyName(t *testing.T) {
	a := assert.New(t)
	demoReader, err := os.Open("demo_app_toggles.json")
	if err != nil {
		t.Fail()
	}
	defer gock.OffAll()

	gock.New("http://foo.com").
		Post("/client/register").
		Reply(200)
	jsonStr, err := read_demo_app_toggles()
	if err != nil {
		t.Fail()
	}

	// Use the string as the body of the Gock request
	gock.New("http://foo.com").
		Get("/client/features").Reply(200).BodyString(jsonStr)
	err = unleash.Initialize(
		unleash.WithListener(&unleash.DebugListener{}),
		unleash.WithAppName("my-application"),
		unleash.WithRefreshInterval(20*time.Second),
		unleash.WithDisableMetrics(true),
		unleash.WithStorage(&unleash.BootstrapStorage{Reader: demoReader}),
		unleash.WithUrl("http://foo.com"),
	)

	if err != nil {
		t.Fail()
	}

	feature := unleash.GetVariant("AuditLog")
	if feature.Enabled == true {
		t.Fatalf("Expected feature to be disabled because Environment does not exist as strategy")
	}
	err = unleash.Close()
	a.Nil(err)
}

func Test_IsEnabledWithUninitializedClient(t *testing.T) {
	result := unleash.IsEnabled("foo", unleash.WithFallback(true))
	if !result {
		t.Fatalf("Expected true")
	}

}
