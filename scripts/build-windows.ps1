$ErrorActionPreference = "Stop"
$ProjectDir = Split-Path -Parent $PSScriptRoot
$BuildDir = Join-Path $ProjectDir "build\desktop-windows"

cmake -S (Join-Path $ProjectDir "desktop-client") -B $BuildDir `
    -DCMAKE_BUILD_TYPE=RelWithDebInfo -DBUILD_TESTING=ON
cmake --build $BuildDir --config RelWithDebInfo --parallel
ctest --test-dir $BuildDir -C RelWithDebInfo --output-on-failure

