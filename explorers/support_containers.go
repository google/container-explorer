/*
Copyright 2021 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package explorers

// KubernetesSupportContainers provides information about supporting containers
// created by Kubernetes.
//
// These containers are created to facilitate Kubernetes operation and do not
// contain customer applications and code.
var KubernetesSupportContainers map[string]string

func init() {
	KubernetesSupportContainers = make(map[string]string)

	loadGKESupportContainers()
}

func loadGKESupportContainers() {
	KubernetesSupportContainers["asia.gcr.io/gke-release-staging/cluster-proportional-autoscaler-amd64"] = "image"
	KubernetesSupportContainers["gcr.io/k8s-ingress-image-push/ingress-gce-404-server-with-metrics"] = "image"
	KubernetesSupportContainers["gke.gcr.io/cluster-proportional-autoscaler"] = "image"
	KubernetesSupportContainers["gke.gcr.io/csi-node-driver-registrar"] = "image"
	KubernetesSupportContainers["gke.gcr.io/event-exporter"] = "image"
	KubernetesSupportContainers["gke.gcr.io/fluent-bit"] = "image"
	KubernetesSupportContainers["gke.gcr.io/fluent-bit-gke-exporter"] = "image"
	KubernetesSupportContainers["gke.gcr.io/gcp-compute-persistent-disk-csi-driver"] = "image"
	KubernetesSupportContainers["gke.gcr.io/gke-metrics-agent"] = "image"
	KubernetesSupportContainers["gke.gcr.io/k8s-dns-dnsmasq-nanny"] = "image"
	KubernetesSupportContainers["gke.gcr.io/k8s-dns-kube-dns"] = "image"
	KubernetesSupportContainers["gke.gcr.io/k8s-dns-sidecar"] = "image"
	KubernetesSupportContainers["gke.gcr.io/kube-proxy-amd64"] = "image"
	KubernetesSupportContainers["gke.gcr.io/prometheus-to-sd"] = "image"
	KubernetesSupportContainers["gke.gcr.io/proxy-agent"] = "image"
	KubernetesSupportContainers["k8s.gcr.io/metrics-server/metrics-server"] = "image"
	KubernetesSupportContainers["k8s.gcr.io/pause"] = "image"
}
