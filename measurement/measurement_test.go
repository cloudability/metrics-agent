// © Copyright Apptio, an IBM Corp. 2024, 2025

package measurement_test

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"testing/quick"
	"time"

	"github.com/cloudability/metrics-agent/measurement"
	"github.com/cloudability/metrics-agent/test"
)

func TestSanity(t *testing.T) {
	t.Parallel()

	tags := make(map[string]string)
	tags["host"] = "macbookpro.Local.abc123"
	tags["cat"] = "dog"

	measure := measurement.Measurement{
		Name:      "ametric",
		Value:     1243.00,
		Timestamp: time.Now().Unix(),
		Tags:      tags,
	}

	strValue := measure.String()

	if !strings.Contains(strValue, "ametric") {
		t.Error("String() should contain the metric name")
	}

	if !strings.Contains(strValue, "1243.00") {
		t.Error("String() should contain the metric value with correct precision")
	}
}

func TestMarshalJSON(t *testing.T) {
	t.Parallel()

	tags := make(map[string]string)
	tags["host"] = "macbookpro.Local.abc123"
	metrics := make(map[string]uint64)
	metrics["uptime"] = 585525897
	randKey := "zz" + test.SecureRandomAlphaString(62) //ensure ordering
	randValue := test.SecureRandomAlphaString(64)
	tags[randKey] = randValue

	t.Run("Measurement with all fields marshals correctly", func(t *testing.T) {
		t.Parallel()

		name := test.SecureRandomAlphaString(64)
		value := rand.Float64()
		ts := time.Now().Unix()

		measure := measurement.Measurement{
			Name:      name,
			Tags:      tags,
			Metrics:   metrics,
			Timestamp: ts,
			Value:     value,
		}

		res, err := json.Marshal(measure)
		jsonString := string(res)

		if err != nil {
			t.Errorf("Encountered error %v", err)
		}

		expected := fmt.Sprintf(
			//nolint lll
			"{\"name\":\"%s\",\"metrics\":{\"uptime\":585525897},\"tags\":{\"host\":\"macbookpro.Local.abc123\",\"%v\":\"%v\"},\"ts\":%d,\"value\":%g}",
			name, randKey, randValue, ts, value)
		if expected != jsonString {
			t.Errorf("expected json does not match actual. expected: %+v actual: %+v", expected, jsonString)
		}
	})

	t.Run("Measurement missing value omits it in JSON", func(t *testing.T) {
		t.Parallel()

		name := test.SecureRandomAlphaString(64)
		ts := time.Now().Unix()

		measure := measurement.Measurement{
			Name:      name,
			Tags:      tags,
			Timestamp: ts,
		}

		res, err := json.Marshal(measure)
		jsonString := string(res)

		if err != nil {
			t.Errorf("Encountered error %v", err)
		}

		if strings.Contains(jsonString, "value") {
			t.Errorf("Encountered unset field where it should have been omitted in output %s", jsonString)
		}
	})

	t.Run("Measurement missing tags omits it in JSON", func(t *testing.T) {
		t.Parallel()

		name := test.SecureRandomAlphaString(64)
		value := rand.Float64()
		ts := time.Now().Unix()

		measure := measurement.Measurement{
			Name:      name,
			Value:     value,
			Timestamp: ts,
		}

		res, err := json.Marshal(measure)
		jsonString := string(res)

		if err != nil {
			t.Errorf("Encountered error %v", err)
		}

		if strings.Contains(jsonString, "tags") {
			t.Errorf("Encountered unset field where it should have been omitted in output %s", jsonString)
		}
	})
}

type testMeasurement measurement.Measurement

func (t testMeasurement) Generate(rand *rand.Rand, size int) reflect.Value {
	tm := testMeasurement{
		Name:      test.SecureRandomAlphaString(size),
		Timestamp: rand.Int63(),
		Value:     rand.Float64(),
	}
	return reflect.ValueOf(tm)
}

func TestMarshalJSON_Blackbox(t *testing.T) {
	t.Parallel()

	assertion := func(m testMeasurement) bool {
		res, err := json.Marshal(m)
		jsonString := string(res)

		if err != nil {
			t.Errorf("Encountered error %v", err)
		}

		return strings.Contains(jsonString, m.Name) &&
			strings.Contains(jsonString, fmt.Sprintf("%g", m.Value)) &&
			strings.Contains(jsonString, strconv.FormatInt(m.Timestamp, 10))
	}

	if err := quick.Check(assertion, nil); err != nil {
		t.Error(err)
	}
}
