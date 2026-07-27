#!/bin/bash

# Copyright 2020 the Velero contributors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -o errexit
set -o nounset
set -o pipefail

# Use /output/usr/bin/ as the default output directory as this
# is the path expected by the Velero Dockerfile.
output_dir=${OUTPUT_DIR:-/output/usr/bin}
restic_bin=${output_dir}/restic
build_path=$(dirname "$PWD")
restic_git_url=${RESTIC_GIT_URL:-https://github.com/restic/restic.git}

if [[ -z "${BIN}" ]]; then
    echo "BIN must be set"
    exit 1
fi

if [[ "${BIN}" != "velero" ]]; then
    echo "${BIN} does not need the restic binary"
    exit 0
fi

if [[ -z "${GOOS}" ]]; then
    echo "GOOS must be set"
    exit 1
fi
if [[ -z "${GOARCH}" ]]; then
    echo "GOARCH must be set"
    exit 1
fi
if [[ -z "${RESTIC_VERSION}" ]]; then
    echo "RESTIC_VERSION must be set"
    exit 1
fi

for attempt in 1 2 3; do
    rm -rf "${build_path}/restic"
    if git clone --depth 1 -b "v${RESTIC_VERSION}" "${restic_git_url}" "${build_path}/restic"; then
        break
    fi
    if [[ "${attempt}" == "3" ]]; then
        echo "Failed to clone restic after ${attempt} attempts" >&2
        exit 1
    fi
    sleep $((attempt * 2))
done
pushd ${build_path}/restic
git apply /go/src/github.com/vmware-tanzu/velero/hack/fix_restic_cve.txt
go run build.go --goos "${GOOS}" --goarch "${GOARCH}" --goarm "${GOARM}" -o ${restic_bin}
chmod +x ${restic_bin}
popd
