/*
Copyright 2019 Bloomberg Finance LP.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package util

import (
	"reflect"
	"strconv"

	solr "github.com/bloomberg/solr-operator/api/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	extv1 "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	SolrMetricsPort        = 8080
	SolrMetricsPortName    = "solr-metrics"
	ExtSolrMetricsPort     = 80
	ExtSolrMetricsPortName = "ext-solr-metrics"

	DefaultPrometheusExporterEntrypoint = "/opt/solr/contrib/prometheus-exporter/bin/solr-exporter"
)

// SolrConnectionInfo defines how to connect to a cloud or standalone solr instance.
// One, and only one, of Cloud or Standalone must be provided.
type SolrConnectionInfo struct {
	CloudZkConnnectionString string
	StandaloneAddress        string
}

// GenerateSolrPrometheusExporterDeployment returns a new appsv1.Deployment pointer generated for the SolrCloud Prometheus Exporter instance
// solrPrometheusExporter: SolrPrometheusExporter instance
func GenerateSolrPrometheusExporterDeployment(solrPrometheusExporter *solr.SolrPrometheusExporter, solrConnectionInfo SolrConnectionInfo) *appsv1.Deployment {
	gracePeriodTerm := int64(10)
	singleReplica := int32(1)
	fsGroup := int64(SolrMetricsPort)

	labels := solrPrometheusExporter.SharedLabelsWith(solrPrometheusExporter.GetLabels())
	selectorLabels := solrPrometheusExporter.SharedLabels()

	labels["technology"] = solr.SolrPrometheusExporterTechnologyLabel
	selectorLabels["technology"] = solr.SolrPrometheusExporterTechnologyLabel

	var solrVolumes []corev1.Volume
	var volumeMounts []corev1.VolumeMount
	exporterArgs := []string{
		"-p", strconv.Itoa(SolrMetricsPort),
		"-n", strconv.Itoa(int(solrPrometheusExporter.Spec.NumThreads)),
	}

	if solrPrometheusExporter.Spec.ScrapeInterval > 0 {
		exporterArgs = append(exporterArgs, "-s", strconv.Itoa(int(solrPrometheusExporter.Spec.ScrapeInterval)))
	}

	// Setup the solrConnectionInfo
	if solrConnectionInfo.CloudZkConnnectionString != "" {
		exporterArgs = append(exporterArgs, "-z", solrConnectionInfo.CloudZkConnnectionString)
	} else if solrConnectionInfo.StandaloneAddress != "" {
		exporterArgs = append(exporterArgs, "-b", solrConnectionInfo.StandaloneAddress)
	}

	// Only add the config if it is passed in from the user. Otherwise, use the default.
	if solrPrometheusExporter.Spec.Config != "" {
		solrVolumes = []corev1.Volume{{
			Name: "solr-prometheus-exporter-xml",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: solrPrometheusExporter.MetricsConfigMapName(),
					},
					Items: []corev1.KeyToPath{
						{
							Key:  "solr-prometheus-exporter.xml",
							Path: "solr-prometheus-exporter.xml",
						},
					},
				},
			},
		}}

		volumeMounts = []corev1.VolumeMount{{Name: "solr-prometheus-exporter-xml", MountPath: "/opt/solr-exporter", ReadOnly: true}}

		exporterArgs = append(exporterArgs, "-f", "/opt/solr-exporter/solr-prometheus-exporter.xml")
	} else {
		exporterArgs = append(exporterArgs, "-f", "/opt/solr/contrib/prometheus-exporter/conf/solr-exporter-config.xml")
	}

	entrypoint := DefaultPrometheusExporterEntrypoint
	if solrPrometheusExporter.Spec.ExporterEntrypoint != "" {
		entrypoint = solrPrometheusExporter.Spec.ExporterEntrypoint
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      solrPrometheusExporter.MetricsDeploymentName(),
			Namespace: solrPrometheusExporter.GetNamespace(),
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: selectorLabels,
			},
			Replicas: &singleReplica,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					TerminationGracePeriodSeconds: &gracePeriodTerm,
					SecurityContext: &corev1.PodSecurityContext{
						FSGroup: &fsGroup,
					},
					Volumes: solrVolumes,
					Containers: []corev1.Container{
						{
							Name:            "solr-prometheus-exporter",
							Image:           solrPrometheusExporter.Spec.Image.ToImageName(),
							ImagePullPolicy: solrPrometheusExporter.Spec.Image.PullPolicy,
							Ports:           []corev1.ContainerPort{{ContainerPort: SolrMetricsPort, Name: SolrMetricsPortName}},
							VolumeMounts:    volumeMounts,
							Command:         []string{entrypoint},
							Args:            exporterArgs,

							LivenessProbe: &corev1.Probe{
								InitialDelaySeconds: 20,
								PeriodSeconds:       10,
								Handler: corev1.Handler{
									HTTPGet: &corev1.HTTPGetAction{
										Scheme: corev1.URISchemeHTTP,
										Path:   "/metrics",
										Port:   intstr.FromInt(SolrMetricsPort),
									},
								},
							},
						},
					},
				},
			},
		},
	}

	if solrPrometheusExporter.Spec.Image.ImagePullSecret != "" {
		deployment.Spec.Template.Spec.ImagePullSecrets = []corev1.LocalObjectReference{
			{Name: solrPrometheusExporter.Spec.Image.ImagePullSecret},
		}
	}

	// Initialize PodOptions if not provided
	if solrPrometheusExporter.Spec.CustomPrometheusKubeOptions.PodOptions == nil {
		solrPrometheusExporter.Spec.CustomPrometheusKubeOptions.PodOptions = &solr.PodOptions{}
	}

	// Apply in customizations if provided by user
	if solrPrometheusExporter.Spec.CustomPrometheusKubeOptions.PodOptions.Affinity != nil {
		deployment.Spec.Template.Spec.Affinity = solrPrometheusExporter.Spec.CustomPrometheusKubeOptions.PodOptions.Affinity
	}

	if solrPrometheusExporter.Spec.CustomPrometheusKubeOptions.PodOptions.Resources.Limits != nil || solrPrometheusExporter.Spec.CustomPrometheusKubeOptions.PodOptions.Resources.Requests != nil {
		deployment.Spec.Template.Spec.Containers[0].Resources = solrPrometheusExporter.Spec.CustomPrometheusKubeOptions.PodOptions.Resources
	}

	return deployment
}

// GenerateConfigMap returns a new corev1.ConfigMap pointer generated for the Solr Prometheus Exporter instance solr-prometheus-exporter.xml
// solrPrometheusExporter: SolrPrometheusExporter instance
func GenerateMetricsConfigMap(solrPrometheusExporter *solr.SolrPrometheusExporter) *corev1.ConfigMap {
	labels := solrPrometheusExporter.SharedLabelsWith(solrPrometheusExporter.GetLabels())

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      solrPrometheusExporter.MetricsConfigMapName(),
			Namespace: solrPrometheusExporter.GetNamespace(),
			Labels:    labels,
		},
		Data: map[string]string{
			"solr-prometheus-exporter.xml": solrPrometheusExporter.Spec.Config,
		},
	}
	return configMap
}

// CopyConfigMapFields copies the owned fields from one ConfigMap to another
func CopyMetricsConfigMapFields(from, to *corev1.ConfigMap) bool {
	requireUpdate := CopyLabelsAndAnnotations(&from.ObjectMeta, &to.ObjectMeta)

	// Don't copy the entire Spec, because we can't overwrite the clusterIp field

	if !reflect.DeepEqual(to.Data, from.Data) {
		requireUpdate = true
	}
	to.Data = from.Data

	return requireUpdate
}

// GenerateSolrMetricsService returns a new corev1.Service pointer generated for the SolrCloud Prometheus Exporter deployment
// Metrics will be collected on this service endpoint, as we don't want to double-tick data if multiple exporters are runnning.
// solrPrometheusExporter: solrPrometheusExporter instance
func GenerateSolrMetricsService(solrPrometheusExporter *solr.SolrPrometheusExporter) *corev1.Service {
	copyLabels := solrPrometheusExporter.GetLabels()
	if copyLabels == nil {
		copyLabels = map[string]string{}
	}
	labels := solrPrometheusExporter.SharedLabelsWith(solrPrometheusExporter.GetLabels())
	labels["service-type"] = "metrics"

	selectorLabels := solrPrometheusExporter.SharedLabels()
	selectorLabels["technology"] = solr.SolrPrometheusExporterTechnologyLabel

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      solrPrometheusExporter.MetricsServiceName(),
			Namespace: solrPrometheusExporter.GetNamespace(),
			Labels:    labels,
			Annotations: map[string]string{
				"prometheus.io/scrape": "true",
				"prometheus.io/scheme": "http",
				"prometheus.io/path":   "/metrics",
				"prometheus.io/port":   strconv.Itoa(ExtSolrMetricsPort),
			},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Name: ExtSolrMetricsPortName, Port: ExtSolrMetricsPort, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt(SolrMetricsPort)},
			},
			Selector: selectorLabels,
		},
	}
	return service
}

// CreateMetricsIngressRule returns a new Ingress Rule generated for the solr metrics endpoint
// This is not currently used, as an ingress is not created for the metrics endpoint.

// solrCloud: SolrCloud instance
// nodeName: string Name of the node
// ingressBaseDomain: string base domain for the ingress controller
func CreateMetricsIngressRule(solrPrometheusExporter *solr.SolrPrometheusExporter, ingressBaseDomain string) extv1.IngressRule {
	externalAddress := solrPrometheusExporter.MetricsIngressUrl(ingressBaseDomain)
	return extv1.IngressRule{
		Host: externalAddress,
		IngressRuleValue: extv1.IngressRuleValue{
			HTTP: &extv1.HTTPIngressRuleValue{
				Paths: []extv1.HTTPIngressPath{
					{
						Backend: extv1.IngressBackend{
							ServiceName: solrPrometheusExporter.MetricsServiceName(),
							ServicePort: intstr.FromInt(ExtSolrMetricsPort),
						},
					},
				},
			},
		},
	}
}
