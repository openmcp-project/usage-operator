package usage

import (
	"encoding/json"
	"errors"

	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/jsonpath"
	"sigs.k8s.io/controller-runtime/pkg/client"

	usagev1alpha1 "github.com/openmcp-project/usage-operator/api/v1alpha1"
)

// NewTraitsExtractor creates a new TraitsExtractor from the given trait definitions.
// This already parses the trait's jsonPath expressions and will return an error if any of the expressions are invalid.
func NewTraitsExtractor(defs map[string]usagev1alpha1.Trait) (*TraitsExtractor, error) {
	res := &TraitsExtractor{
		defs: make(map[string]PreparedTrait, len(defs)),
	}

	for name, def := range defs {
		jp := jsonpath.New(name).AllowMissingKeys(true)
		if err := jp.Parse(fmt.Sprintf("{%s}", def.Path)); err != nil {
			return nil, fmt.Errorf("error parsing jsonPath expression for trait '%s': %w", name, err)
		}
		res.defs[name] = PreparedTrait{
			Trait:      def,
			parsedPath: jp,
		}
	}

	return res, nil
}

type PreparedTrait struct {
	usagev1alpha1.Trait

	parsedPath *jsonpath.JSONPath
}

// TraitsExtractor is a utility struct that provides methods to extract trait information from Kubernetes objects.
type TraitsExtractor struct {
	defs map[string]PreparedTrait
}

// ExtractTraits extracts the traits from the given Kubernetes object and its namespace. It returns a map of trait names to their corresponding values (in JSON representation).
func (te *TraitsExtractor) ExtractTraits(obj client.Object, ns *corev1.Namespace) (map[string][]byte, error) {
	rawObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, fmt.Errorf("error converting object to unstructured: %w", err)
	}
	var rawNs map[string]any
	if ns != nil {
		rawNs, err = runtime.DefaultUnstructuredConverter.ToUnstructured(ns)
		if err != nil {
			return nil, fmt.Errorf("error converting namespace to unstructured: %w", err)
		}
	}

	data := map[string]any{
		"resource":  rawObj,
		"namespace": rawNs,
	}

	var errs error
	res := make(map[string][]byte, len(te.defs))
	for name, def := range te.defs {
		results, err := def.parsedPath.FindResults(data)
		if err != nil {
			err = fmt.Errorf("error executing jsonPath expression for trait '%s': %w", name, err)
			errs = errors.Join(errs, err)
			res[name] = errorJson(err)
			continue
		}
		// FindResults returns [][]reflect.Value; the inner slice holds all matched values.
		var value any
		if len(results) > 0 && len(results[0]) > 0 {
			if len(results[0]) == 1 {
				value = results[0][0].Interface()
			} else {
				values := make([]any, len(results[0]))
				for i, v := range results[0] {
					values[i] = v.Interface()
				}
				value = values
			}
		}
		raw, err := json.Marshal(value)
		if err != nil {
			err = fmt.Errorf("error marshaling trait '%s' value: %w", name, err)
			errs = errors.Join(errs, err)
			res[name] = errorJson(err)
			continue
		}
		res[name] = raw
	}

	return res, errs
}

func errorJson(err error) []byte {
	return fmt.Appendf(nil, "{\"error\": \"%s\"}", err.Error())
}
