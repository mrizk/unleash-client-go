package unleash

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Unleash/unleash-client-go/v4/api"
	internalapi "github.com/Unleash/unleash-client-go/v4/internal/api"
	"github.com/h2non/gock"
	"github.com/nbio/st"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestMetrics_RegisterInstance(t *testing.T) {
	assert := assert.New(t)
	defer gock.OffAll()

	gock.New(mockerServer).
		Post("/client/register").
		MatchHeader("UNLEASH-APPNAME", mockAppName).
		MatchHeader("UNLEASH-INSTANCEID", mockInstanceId).
		Reply(200)

	gock.New(mockerServer).
		Get("/client/features").
		Reply(200).
		JSON(api.FeatureResponse{})

	mockListener := &MockedListener{}
	mockListener.On("OnReady").Return()
	mockListener.On("OnRegistered", mock.AnythingOfType("ClientData"))

	client, err := NewClient(
		WithUrl(mockerServer),
		WithAppName(mockAppName),
		WithInstanceId(mockInstanceId),
		WithListener(mockListener),
	)

	time.Sleep(1 * time.Second)
	client.Close()

	assert.Nil(err, "client should not return an error")

	assert.True(gock.IsDone(), "there should be no more mocks")
}

func TestMetrics_VariantsCountToggles(t *testing.T) {
	assert := assert.New(t)
	defer gock.OffAll()

	gock.New(mockerServer).
		Post("/client/register").
		MatchHeader("UNLEASH-APPNAME", mockAppName).
		MatchHeader("UNLEASH-INSTANCEID", mockInstanceId).
		Reply(200)

	gock.New(mockerServer).
		Get("/client/features").
		Reply(200).
		JSON(api.FeatureResponse{})

	mockListener := &MockedListener{}
	mockListener.On("OnReady").Return()
	mockListener.On("OnCount", "foo", false).Return()
	mockListener.On("OnRegistered", mock.AnythingOfType("ClientData"))

	client, err := NewClient(
		WithUrl(mockerServer),
		WithAppName(mockAppName),
		WithInstanceId(mockInstanceId),
		WithListener(mockListener),
	)

	client.WaitForReady()
	client.GetVariant("foo")

	assert.EqualValues(client.metrics.bucket.Toggles["foo"].No, 1)
	client.Close()

	assert.Nil(err, "client should not return an error")
	assert.True(gock.IsDone(), "there should be no more mocks")
}

func TestMetrics_DoPost(t *testing.T) {
	assert := assert.New(t)
	defer gock.OffAll()

	gock.New(mockerServer).
		Post("/client/register").
		Reply(200)

	gock.New(mockerServer).
		Get("/client/features").
		Reply(200).
		JSON(api.FeatureResponse{})

	gock.New(mockerServer).
		Post("").
		MatchHeader("UNLEASH-APPNAME", mockAppName).
		MatchHeader("UNLEASH-INSTANCEID", mockInstanceId).
		Reply(200)

	mockListener := &MockedListener{}
	mockListener.On("OnReady").Return()
	mockListener.On("OnRegistered", mock.AnythingOfType("ClientData"))

	client, err := NewClient(
		WithUrl(mockerServer),
		WithAppName(mockAppName),
		WithInstanceId(mockInstanceId),
		WithListener(&DebugListener{}),
	)

	assert.Nil(err, "client should not return an error")

	m := client.metrics

	serverUrl, _ := url.Parse(mockerServer)
	res, err := m.doPost(serverUrl, &struct{}{})
	client.Close()

	assert.Nil(err, "doPost should not return an error")
	assert.Equal(200, res.StatusCode, "statusCode should be 200")
	assert.True(gock.IsDone(), "there should be no more mocks")
}

func TestMetrics_DisabledMetrics(t *testing.T) {
	assert := assert.New(t)
	defer gock.OffAll()

	gock.New(mockerServer).
		Get("/client/features").
		Reply(200).
		JSON(api.FeatureResponse{})

	mockListener := &MockedListener{}
	mockListener.On("OnReady").Return()

	client, err := NewClient(
		WithUrl(mockerServer),
		WithDisableMetrics(true),
		WithMetricsInterval(100*time.Millisecond),
		WithAppName(mockAppName),
		WithInstanceId(mockInstanceId),
		WithListener(mockListener),
	)
	assert.Nil(err, "client should not return an error")

	client.WaitForReady()
	client.IsEnabled("foo")
	client.IsEnabled("bar")
	client.IsEnabled("baz")

	time.Sleep(300 * time.Millisecond)
	client.Close()
	assert.True(gock.IsDone(), "there should be no more mocks")
}

// TestMetrics_SendMetricsFail tests that no metrics are lost if /client/metrics
// fails temporarily.
func TestMetrics_SendMetricsFail(t *testing.T) {
	assert := assert.New(t)

	type metricsReq struct {
		// toggles are the toggles sent to /client/metrics
		toggles map[string]internalapi.ToggleCount

		// status is the status code returned from /client/metrics
		status int
	}
	metricsCalls := make(chan metricsReq, 10)
	var prevToggles map[string]internalapi.ToggleCount
	var sendStatus200 int32
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		switch req.Method + " " + req.URL.Path {
		case "POST /client/register":
		case "GET /client/features":
			writeJSON(rw, api.FeatureResponse{})
		case "POST /client/metrics":
			body, err := ioutil.ReadAll(req.Body)
			assert.Nil(err)
			status200 := atomic.LoadInt32(&sendStatus200) == 1
			status := 400
			if status200 {
				status = 200
			}

			var md MetricsData
			err = json.Unmarshal(body, &md)
			assert.Nil(err)

			if status200 || !reflect.DeepEqual(md.Bucket.Toggles, prevToggles) {
				prevToggles = md.Bucket.Toggles
				metricsCalls <- metricsReq{md.Bucket.Toggles, status}
			}
			rw.WriteHeader(status)
		default:
			t.Fatalf("Unexpected request: %+v", req)
		}
	}))
	defer srv.Close()

	mockListener := &MockedListener{}
	mockListener.On("OnReady").Return()
	mockListener.On("OnRegistered", mock.AnythingOfType("ClientData"))
	mockListener.On("OnCount", "foo", true).Return()
	mockListener.On("OnCount", "foo", false).Return()
	mockListener.On("OnWarning", mock.MatchedBy(func(e error) bool {
		return strings.HasSuffix(e.Error(), "/client/metrics return 400")
	})).Return()
	mockListener.On("OnSent", mock.AnythingOfType("MetricsData")).Return()
	client, err := NewClient(
		WithUrl(srv.URL),
		WithAppName(mockAppName),
		WithInstanceId(mockInstanceId),
		WithListener(mockListener),
		WithMetricsInterval(time.Millisecond),
	)
	assert.Nil(err, "client should not return an error")
	client.WaitForReady()

	ck := func(status int, yes, no int32, r metricsReq) {
		t.Helper()
		assert.Equal(status, r.status)
		assert.Equal(yes, r.toggles["foo"].Yes)
		assert.Equal(no, r.toggles["foo"].No)
	}
	m := client.metrics

	// /client/metrics returns 400, check that the counts aren't reset.
	m.count("foo", true)
	ck(400, 1, 0, <-metricsCalls)
	m.count("foo", false)
	ck(400, 1, 1, <-metricsCalls)
	m.count("foo", true)
	ck(400, 2, 1, <-metricsCalls)

	mockListener.AssertNotCalled(t, "OnSent", mock.AnythingOfType("MetricsData"))

	atomic.StoreInt32(&sendStatus200, 1)
	ck(200, 2, 1, <-metricsCalls)

	// As /client/metrics returned 200 and m.count hasn't been called again
	// there are no more metrics to report and thus /client/metrics
	// shouldn't be called again.
	select {
	case r := <-metricsCalls:
		t.Fatalf("Didn't expect request to /client/metrics, got %+v", r)
	case <-time.NewTimer(500 * time.Millisecond).C:
	}
	client.Close()

	// Now OnSent should have been called as /client/metrics returned 200.
	mockListener.AssertCalled(t, "OnSent", mock.AnythingOfType("MetricsData"))
}

func TestMetrics_ShouldNotCountMetricsForParentToggles(t *testing.T) {
	assert := assert.New(t)
	defer gock.OffAll()

	gock.New(mockerServer).
		Post("/client/register").
		Reply(200)

	gock.New(mockerServer).
		Get("/client/features").
		Reply(200).
		JSON(api.FeatureResponse{
			Features: []api.Feature{
				{
					Name:        "parent",
					Enabled:     true,
					Description: "parent toggle",
					Strategies: []api.Strategy{
						{
							Id:          1,
							Name:        "flexibleRollout",
							Constraints: []api.Constraint{},
							Parameters: map[string]interface{}{
								"rollout":    100,
								"stickiness": "default",
							},
						},
					},
				},
				{
					Name:        "child",
					Enabled:     true,
					Description: "parent toggle",
					Strategies: []api.Strategy{
						{
							Id:          1,
							Name:        "flexibleRollout",
							Constraints: []api.Constraint{},
							Parameters: map[string]interface{}{
								"rollout":    100,
								"stickiness": "default",
							},
						},
					},
					Dependencies: &[]api.Dependency{
						{
							Feature: "parent",
						},
					},
				},
			},
		})

	mockListener := &MockedListener{}
	mockListener.On("OnReady").Return()
	mockListener.On("OnError").Return()
	mockListener.On("OnRegistered", mock.AnythingOfType("ClientData"))
	mockListener.On("OnCount", "child", true).Return()

	client, err := NewClient(
		WithUrl(mockerServer),
		WithAppName(mockAppName),
		WithInstanceId(mockInstanceId),
		WithListener(mockListener),
	)
	assert.Nil(err, "client should not return an error")
	client.WaitForReady()
	client.IsEnabled("child")

	assert.EqualValues(client.metrics.bucket.Toggles["child"].Yes, 1)
	assert.EqualValues(client.metrics.bucket.Toggles["parent"].Yes, 0)
	err = client.Close()

	assert.Nil(err, "client should not return an error")
	assert.True(gock.IsDone(), "there should be no more mocks")
}

func TestMetrics_ShouldBackoffOn500(t *testing.T) {
	assert := assert.New(t)
	defer gock.OffAll()

	gock.New(mockerServer).
		Post("/client/register").
		Reply(200)
	gock.New(mockerServer).
		Post("/client/metrics").
		Persist().
		Reply(500)
	gock.New(mockerServer).
		Get("/client/features").
		Reply(200).
		JSON(api.FeatureResponse{})
	mockListener := &MockedListener{}
	mockListener.On("OnReady").Return()
	mockListener.On("OnRegistered", mock.AnythingOfType("ClientData")).Return()
	mockListener.On("OnCount", "foo", false).Return()
	mockListener.On("OnCount", "bar", false).Return()
	mockListener.On("OnCount", "baz", false).Return()
	mockListener.On("OnWarning", mock.MatchedBy(func(e error) bool {
		return strings.HasSuffix(e.Error(), "http://foo.com/client/metrics return 500")
	})).Return()
	mockListener.On("OnError", mock.Anything).Return()

	client, err := NewClient(
		WithUrl(mockerServer),
		WithMetricsInterval(50*time.Millisecond),
		WithAppName(mockAppName),
		WithInstanceId(mockInstanceId),
		WithListener(mockListener),
	)
	assert.Nil(err, "client should not return an error")

	client.WaitForReady()
	client.IsEnabled("foo")
	client.IsEnabled("bar")
	client.IsEnabled("baz")

	time.Sleep(320 * time.Millisecond)
	err = client.Close()
	assert.Equal(float64(3), client.metrics.errors)
	assert.Nil(err, "Client should close without a problem")

}

func TestMetrics_ErrorCountShouldDecreaseIfSuccessful(t *testing.T) {
	assert := assert.New(t)
	defer gock.OffAll()

	gock.New(mockerServer).
		Post("/client/register").
		Reply(200)
	gock.New(mockerServer).
		Post("/client/metrics").
		Times(2).
		Reply(500)
	gock.New(mockerServer).
		Get("/client/features").
		Reply(200).
		JSON(api.FeatureResponse{})
	gock.New(mockerServer).
		Post("/client/metrics").
		Persist().
		Reply(200)

	client, err := NewClient(
		WithUrl(mockerServer),
		WithMetricsInterval(50*time.Millisecond),
		WithAppName(mockAppName),
		WithInstanceId(mockInstanceId),
	)
	assert.Nil(err, "client should not return an error")

	client.WaitForReady()
	client.IsEnabled("foo")
	client.IsEnabled("bar")
	client.IsEnabled("baz")
	time.Sleep(360 * time.Millisecond)
	client.IsEnabled("foo")
	time.Sleep(100 * time.Millisecond)
	err = client.Close()
	assert.Equal(float64(0), client.metrics.errors)
	assert.Nil(err, "Client should close without a problem")
}

func clientDataMatcher() func(req *http.Request, ereq *gock.Request) (bool, error) {
	defaultStrategies := []string{
		"default",
		"applicationHostname",
		"gradualRolloutRandom",
		"gradualRolloutSessionId",
		"gradualRolloutUserId",
		"remoteAddress",
		"userWithId",
		"flexibleRollout",
	}
	return func(req *http.Request, ereq *gock.Request) (bool, error) {
		var data ClientData
		err := json.NewDecoder(req.Body).Decode(&data)
		if err != nil {
			return false, err
		}

		if data.Started.IsZero() {
			return false, nil
		}

		expectedData := ClientData{
			AppName:          mockAppName,
			InstanceID:       mockInstanceId,
			SDKVersion:       fmt.Sprintf("%s:%s", clientName, clientVersion),
			Strategies:       defaultStrategies,
			Interval:         0,
			PlatformVersion:  runtime.Version(),
			PlatformName:     "go",
			YggdrasilVersion: nil,
			SpecVersion:      specVersion,
		}

		return data.AppName == expectedData.AppName &&
			data.InstanceID == expectedData.InstanceID &&
			data.SDKVersion == expectedData.SDKVersion &&
			compareStringSlices(data.Strategies, expectedData.Strategies) &&
			data.Interval == expectedData.Interval &&
			data.PlatformVersion == expectedData.PlatformVersion &&
			data.PlatformName == expectedData.PlatformName &&
			data.YggdrasilVersion == expectedData.YggdrasilVersion &&
			data.SpecVersion == expectedData.SpecVersion, nil
	}
}

func compareStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestMetrics_ClientDataIncludesNewMetadata(t *testing.T) {
	assert := assert.New(t)
	defer gock.OffAll()

	gock.New(mockerServer).
		Post("/client/register").
		AddMatcher(clientDataMatcher()).
		Reply(200)

	client, err := NewClient(
		WithUrl(mockerServer),
		WithMetricsInterval(50*time.Millisecond),
		WithAppName(mockAppName),
		WithInstanceId(mockInstanceId),
	)

	assert.Nil(err, "Client should open without a problem")

	time.Sleep(100 * time.Millisecond)

	err = client.Close()

	assert.Nil(err, "Client should close without errors")

	st.Expect(t, gock.IsDone(), true)
}

func TestMetrics_metricsData_includes_new_metadata(t *testing.T) {
	assert := assert.New(t)
	defer gock.OffAll()

	gock.Observe(gock.DumpRequest)

	gock.New(mockerServer).Post("/client/register").Reply(200)
	gock.New(mockerServer).
		Post("/client/metrics").
		BodyString(`.*platformVersion.*`).
		Reply(200)
	gock.New(mockerServer).
		Get("/client/features").
		Reply(200).
		JSON(api.FeatureResponse{
			Features: []api.Feature{
				{
					Name:        "parent",
					Enabled:     true,
					Description: "parent toggle",
					Strategies: []api.Strategy{
						{
							Id:          1,
							Name:        "flexibleRollout",
							Constraints: []api.Constraint{},
							Parameters: map[string]interface{}{
								"rollout":    100,
								"stickiness": "default",
							},
						},
					},
				},
				{
					Name:        "child",
					Enabled:     true,
					Description: "parent toggle",
					Strategies: []api.Strategy{
						{
							Id:          1,
							Name:        "flexibleRollout",
							Constraints: []api.Constraint{},
							Parameters: map[string]interface{}{
								"rollout":    100,
								"stickiness": "default",
							},
						},
					},
					Dependencies: &[]api.Dependency{
						{
							Feature: "parent",
						},
					},
				},
			},
		})

	client, err := NewClient(
		WithUrl(mockerServer),
		WithMetricsInterval(50*time.Millisecond),
		WithAppName(mockAppName),
		WithInstanceId(mockInstanceId),
		WithDisableMetrics(false),
	)
	assert.Nil(err, "Client should open without a problem")

	client.WaitForReady()
	client.IsEnabled("foo")
	client.IsEnabled("bar")
	client.IsEnabled("baz")

	time.Sleep(320 * time.Millisecond)
	err = client.Close()

	assert.Nil(err, "Client should close without errors")

	st.Expect(t, gock.IsDone(), true)
}
