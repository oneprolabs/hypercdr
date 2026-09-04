# Native Kubernetes provider. Keep this path deliberately small and compatible
# with the registration flow that predates cloud-provider adapters.
provider_native_kubernetes_prepare_dependencies() { :; }
provider_native_kubernetes_select_context() { :; }
provider_native_kubernetes_align_kubectl_version() { :; }
provider_native_kubernetes_verify() { :; }
provider_native_kubernetes_prepare_platform_trust() {
  PLATFORM_TLS_SKIP_VERIFY="true"
  platform_ca_file=""
}
provider_native_kubernetes_prepare_preflight() { :; }
provider_native_kubernetes_run_preflight() { :; }
provider_native_kubernetes_install_platform_trust() { :; }
provider_native_kubernetes_install_backup_backend() { :; }
provider_native_kubernetes_rollback_backup_backend() { :; }
