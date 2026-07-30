#!/usr/bin/env bash
set -Eeuo pipefail

project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
build_dir="${project_dir}/build/desktop-linux"
local_deps="${project_dir}/.deps/root/usr"

command -v cmake >/dev/null || { echo "缺少 cmake"; exit 1; }
command -v ninja >/dev/null || { echo "缺少 ninja-build"; exit 1; }

cmake_args=(
    -S "${project_dir}/desktop-client"
    -B "${build_dir}"
    -G Ninja
    -DCMAKE_BUILD_TYPE=RelWithDebInfo
    -DBUILD_TESTING=ON
)
if [[ -d "${local_deps}" ]]; then
    cmake_args+=("-DCMAKE_PREFIX_PATH=${local_deps}")
    cmake_args+=("-DCMAKE_INCLUDE_PATH=${local_deps}/include")
    cmake_args+=("-DCMAKE_LIBRARY_PATH=${local_deps}/lib/x86_64-linux-gnu")
    websocket_cmake="$(find "${local_deps}/lib" -type d -path '*/cmake/Qt6WebSockets' -print -quit)"
    if [[ -n "${websocket_cmake}" ]]; then
        cmake_args+=("-DQt6WebSockets_DIR=${websocket_cmake}")
    fi
fi
cmake "${cmake_args[@]}"
cmake --build "${build_dir}" --parallel
ctest --test-dir "${build_dir}" --output-on-failure
