package usage_test

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	usagev1alpha1 "github.com/openmcp-project/usage-operator/api/v1alpha1"
	"github.com/openmcp-project/usage-operator/internal/usage"
)

type TestResource struct {
	metav1.TypeMeta    `json:",inline"`
	*metav1.ObjectMeta `json:"metadata,omitempty"`
	Object             *TestResource            `json:"object,omitempty"`
	List               []*TestResource          `json:"list,omitempty"`
	Map                map[string]*TestResource `json:"map,omitempty"`
	String             *string                  `json:"string,omitempty"`
	Number             *int                     `json:"number,omitempty"`
	Boolean            *bool                    `json:"boolean,omitempty"`
}

// DeepCopyObject implements [client.Object].
func (t *TestResource) DeepCopyObject() runtime.Object {
	if t == nil {
		return nil
	}
	out := &TestResource{
		TypeMeta:   t.TypeMeta,
		ObjectMeta: t.DeepCopy(),
	}
	if t.Object != nil {
		out.Object = t.Object.DeepCopyObject().(*TestResource)
	}
	if t.List != nil {
		out.List = make([]*TestResource, len(t.List))
		for i, item := range t.List {
			if item != nil {
				out.List[i] = item.DeepCopyObject().(*TestResource)
			}
		}
	}
	if t.Map != nil {
		out.Map = make(map[string]*TestResource, len(t.Map))
		for k, v := range t.Map {
			if v != nil {
				out.Map[k] = v.DeepCopyObject().(*TestResource)
			}
		}
	}
	if t.String != nil {
		s := *t.String
		out.String = &s
	}
	if t.Number != nil {
		n := *t.Number
		out.Number = &n
	}
	if t.Boolean != nil {
		b := *t.Boolean
		out.Boolean = &b
	}
	return out
}

var _ client.Object = &TestResource{}

var _ = Describe("Traits Extraction", func() {

	var testObj *TestResource
	var testNs *corev1.Namespace
	BeforeEach(func() {
		testObj = &TestResource{
			Object: &TestResource{
				Object: &TestResource{
					String: new("double nested"),
				},
				String: new("nested"),
			},
			List: []*TestResource{
				{
					String: new("list item 1"),
				},
				{
					String: new("list item 2"),
				},
			},
			Map: map[string]*TestResource{
				"foo": {
					String: new("fooValue"),
				},
				"bar": {
					String: new("barValue"),
				},
			},
			String:  new("top level"),
			Number:  new(5),
			Boolean: new(true),
		}
		testNs = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
				Annotations: map[string]string{
					"ann1": "annVal1",
					"ann2": "annVal2",
				},
				Labels: map[string]string{
					"label1": "labelVal1",
					"label2": "labelVal2",
				},
			},
			Spec: corev1.NamespaceSpec{
				Finalizers: []corev1.FinalizerName{
					"kubernetes",
				},
			},
			Status: corev1.NamespaceStatus{
				Phase: corev1.NamespaceActive,
				Conditions: []corev1.NamespaceCondition{
					{
						Type:    "FooCondition",
						Status:  corev1.ConditionTrue,
						Reason:  "FooReason",
						Message: "This is foo.",
					},
					{
						Type:    "BarCondition",
						Status:  corev1.ConditionFalse,
						Reason:  "BarReason",
						Message: "This is bar.",
					},
				},
			},
		}
	})

	It("should correctly extract traits", func() {
		te, err := usage.NewTraitsExtractor(map[string]usagev1alpha1.Trait{
			// top-level resource fields
			"resource-object":  {Path: ".resource.object"},
			"resource-list":    {Path: ".resource.list"},
			"resource-map":     {Path: ".resource.map"},
			"resource-string":  {Path: ".resource.string"},
			"resource-number":  {Path: ".resource.number"},
			"resource-boolean": {Path: ".resource.boolean"},
			// nested resource fields
			"resource-object-string":        {Path: ".resource.object.string"},
			"resource-object-object-string": {Path: ".resource.object.object.string"},
			"resource-list-first-string":    {Path: ".resource.list[0].string"},
			"resource-map-foo-string":       {Path: ".resource.map['foo'].string"},
			// namespace fields
			"namespace-name":        {Path: ".namespace.metadata.name"},
			"namespace-annotations": {Path: ".namespace.metadata.annotations"},
			"namespace-labels":      {Path: ".namespace.metadata.labels"},
			"namespace-label-foo":   {Path: ".namespace.metadata.labels['label1']"},
			"namespace-phase":       {Path: ".namespace.status.phase"},
			"namespace-finalizers":  {Path: ".namespace.spec.finalizers"},
			// namespace conditions using '@' filter syntax
			"namespace-foo-condition-status": {Path: ".namespace.status.conditions[?(@.type=='FooCondition')].status"},
			"namespace-foo-condition-reason": {Path: ".namespace.status.conditions[?(@.type=='FooCondition')].reason"},
			"namespace-bar-condition-status": {Path: ".namespace.status.conditions[?(@.type=='BarCondition')].status"},
			"namespace-all-condition-types":  {Path: ".namespace.status.conditions[*].type"},
		})
		Expect(err).NotTo(HaveOccurred())
		extracted, err := te.ExtractTraits(testObj, testNs)
		Expect(err).NotTo(HaveOccurred())

		// top-level resource fields
		Expect(extracted["resource-object"]).To(MatchJSON(toJson(testObj.Object)))
		Expect(extracted["resource-list"]).To(MatchJSON(toJson(testObj.List)))
		Expect(extracted["resource-map"]).To(MatchJSON(toJson(testObj.Map)))
		Expect(extracted["resource-string"]).To(MatchJSON(toJson(testObj.String)))
		Expect(extracted["resource-number"]).To(MatchJSON(toJson(testObj.Number)))
		Expect(extracted["resource-boolean"]).To(MatchJSON(toJson(testObj.Boolean)))

		// nested resource fields
		Expect(extracted["resource-object-string"]).To(MatchJSON(toJson(testObj.Object.String)))
		Expect(extracted["resource-object-object-string"]).To(MatchJSON(toJson(testObj.Object.Object.String)))
		Expect(extracted["resource-list-first-string"]).To(MatchJSON(toJson(testObj.List[0].String)))
		Expect(extracted["resource-map-foo-string"]).To(MatchJSON(toJson(testObj.Map["foo"].String)))

		// namespace fields
		Expect(extracted["namespace-name"]).To(MatchJSON(toJson(testNs.Name)))
		Expect(extracted["namespace-annotations"]).To(MatchJSON(toJson(testNs.Annotations)))
		Expect(extracted["namespace-labels"]).To(MatchJSON(toJson(testNs.Labels)))
		Expect(extracted["namespace-label-foo"]).To(MatchJSON(toJson(testNs.Labels["label1"])))
		Expect(extracted["namespace-phase"]).To(MatchJSON(toJson(testNs.Status.Phase)))
		Expect(extracted["namespace-finalizers"]).To(MatchJSON(toJson(testNs.Spec.Finalizers)))

		// namespace conditions via '@' filter
		Expect(extracted["namespace-foo-condition-status"]).To(MatchJSON(toJson(string(testNs.Status.Conditions[0].Status))))
		Expect(extracted["namespace-foo-condition-reason"]).To(MatchJSON(toJson(testNs.Status.Conditions[0].Reason)))
		Expect(extracted["namespace-bar-condition-status"]).To(MatchJSON(toJson(string(testNs.Status.Conditions[1].Status))))
		Expect(extracted["namespace-all-condition-types"]).To(MatchJSON(toJson([]string{
			string(testNs.Status.Conditions[0].Type),
			string(testNs.Status.Conditions[1].Type),
		})))
	})

})

func toJson(obj any) []byte {
	data, err := json.Marshal(obj)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	return data
}
