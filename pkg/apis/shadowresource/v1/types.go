package v1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// These const variables are used in our custom controller.
const (
	GroupName string = "burghardt.tech"
	Kind      string = "Shadow"
	Version   string = "v1"
	Plural    string = "shadows"
	Singluar  string = "shadow"
	ShortName string = "sh"
	Name      string = Plural + "." + GroupName
)

type ShadowSepc struct {
	PodName        string `json:"podName"`
	Image string `json:"image"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type Shadow struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ShadowSepc `json:"spec"`
	Status string      `json:"status"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type ShadowList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Shadow `json:"items"`
}