#!/usr/bin/env python3
"""Assert the securityContext invariants on the platform manifests.

`kubectl kustomize` rendering cleanly proves the YAML parses — not that the
hardening fields are present. This is the assertion: it parses every Deployment
and Job under infra/k8s/platform/ and fails if a workload is missing a control
it must have, so a new deployment added without securityContext is caught rather
than shipping unhardened.

Invariants:
  * UNIVERSAL — every container carries allowPrivilegeEscalation:false and
    seccompProfile.type == RuntimeDefault. No exceptions.
  * NON-ROOT — every workload runs as runAsNonRoot at the pod level, EXCEPT the
    documented set below (postgres needs root for initdb; the stock nginx gateway
    binds :80). A new workload must either be non-root or be added here on
    purpose — which forces the reader to make the call.

Pure stdlib + PyYAML; no cluster, no apply.
"""
import glob
import os
import sys

try:
    import yaml
except ImportError:
    sys.exit("PyYAML required: pip install pyyaml")

# Workloads intentionally NOT runAsNonRoot (root genuinely required); see
# docs/SECURITY_REVIEW_2026-09.md and the in-manifest comments.
RUNASNONROOT_EXEMPT = {"postgres", "gateway"}

PLATFORM = os.path.join(os.path.dirname(__file__), "..", "infra", "k8s", "platform")


def containers_of(spec):
    return spec.get("containers", []) + spec.get("initContainers", [])


def main():
    failures = []
    checked = 0
    for path in sorted(glob.glob(os.path.join(PLATFORM, "*.yaml"))):
        with open(path) as fh:
            for doc in yaml.safe_load_all(fh):
                if not doc or doc.get("kind") not in ("Deployment", "Job"):
                    continue
                name = doc["metadata"]["name"]
                pspec = doc["spec"]["template"]["spec"]
                podsc = pspec.get("securityContext", {}) or {}
                checked += 1

                # NON-ROOT invariant (with the documented exemptions).
                if name not in RUNASNONROOT_EXEMPT and not podsc.get("runAsNonRoot"):
                    failures.append(f"{name}: pod securityContext missing runAsNonRoot (add it, or add {name!r} to RUNASNONROOT_EXEMPT with a reason)")

                # seccompProfile applies to all containers when set at the pod level,
                # so a pod-level RuntimeDefault satisfies the invariant for each.
                pod_seccomp = (podsc.get("seccompProfile", {}) or {}).get("type") == "RuntimeDefault"

                # UNIVERSAL invariant on every container.
                for c in containers_of(pspec):
                    csc = c.get("securityContext", {}) or {}
                    cname = c.get("name", "?")
                    if csc.get("allowPrivilegeEscalation") is not False:
                        failures.append(f"{name}/{cname}: container missing allowPrivilegeEscalation: false")
                    if not pod_seccomp and (csc.get("seccompProfile", {}) or {}).get("type") != "RuntimeDefault":
                        failures.append(f"{name}/{cname}: missing seccompProfile RuntimeDefault (pod or container level)")

    if failures:
        print("securityContext verification FAILED:")
        for f in failures:
            print("  - " + f)
        sys.exit(1)
    print(f"securityContext OK: {checked} workloads verified across the platform manifests")


if __name__ == "__main__":
    main()
