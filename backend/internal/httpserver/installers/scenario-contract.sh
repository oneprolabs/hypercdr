# Registration scenario contract. This entry point intentionally performs only
# a fresh registration. Upgrade, handover, online unregister and offline
# uninstall have independent endpoints/commands and cannot silently enter this
# mutation path.
validate_registration_scenario() {
  case "$SCENARIO" in
    fresh-install) return 0 ;;
    *) fail "Unsupported registration scenario '${SCENARIO}'. Use the dedicated HyperCDR workflow for upgrade, handover, reinstall, or uninstall." 2 ;;
  esac
}

