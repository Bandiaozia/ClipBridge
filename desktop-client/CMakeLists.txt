cmake_minimum_required(VERSION 3.24)
project(ClipBridgeDesktop VERSION 0.3.0 LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)
set(CMAKE_AUTOMOC ON)
set(CMAKE_AUTOUIC ON)
set(CMAKE_AUTORCC ON)

find_package(Qt6 6.4 REQUIRED COMPONENTS Widgets Network WebSockets Sql Test)
find_path(SODIUM_INCLUDE_DIR sodium.h REQUIRED)
find_library(SODIUM_LIBRARY NAMES sodium libsodium REQUIRED)
find_path(QRENCODE_INCLUDE_DIR qrencode.h REQUIRED)
find_library(QRENCODE_LIBRARY NAMES qrencode
    PATH_SUFFIXES lib lib/x86_64-linux-gnu REQUIRED)

set(CLIPBRIDGE_CORE_SOURCES
    src/ApiClient.cpp src/ApiClient.h
    src/AndroidUsbConnector.cpp src/AndroidUsbConnector.h
    src/AutoStartManager.cpp src/AutoStartManager.h
    src/ClipboardManager.cpp src/ClipboardManager.h
    src/ClipboardSyncService.cpp src/ClipboardSyncService.h
    src/CryptoManager.cpp src/CryptoManager.h
    src/DeviceManager.cpp src/DeviceManager.h
    src/HistoryRepository.cpp src/HistoryRepository.h
    src/Models.h
    src/NotificationManager.cpp src/NotificationManager.h
    src/PairingManager.cpp src/PairingManager.h
    src/SecureStorage.cpp src/SecureStorage.h
    src/SensitiveContentDetector.cpp src/SensitiveContentDetector.h
    src/SettingsRepository.cpp src/SettingsRepository.h
    src/TokenManager.cpp src/TokenManager.h
    src/WebSocketClient.cpp src/WebSocketClient.h
)

add_library(clipbridge_core ${CLIPBRIDGE_CORE_SOURCES})
target_include_directories(clipbridge_core PUBLIC src PRIVATE
    ${SODIUM_INCLUDE_DIR} ${QRENCODE_INCLUDE_DIR})
target_link_libraries(clipbridge_core PUBLIC
    Qt6::Widgets Qt6::Network Qt6::WebSockets Qt6::Sql
    ${SODIUM_LIBRARY} ${QRENCODE_LIBRARY})
target_compile_definitions(clipbridge_core PRIVATE
    QT_NO_CAST_FROM_ASCII QT_NO_CAST_TO_ASCII)
if(MSVC)
    target_compile_options(clipbridge_core PRIVATE /W4 /permissive-)
else()
    target_compile_options(clipbridge_core PRIVATE -Wall -Wextra -Wpedantic)
endif()
if(WIN32)
    target_link_libraries(clipbridge_core PRIVATE Advapi32)
endif()

add_executable(clipbridge-desktop
    src/AppController.cpp src/AppController.h
    src/main.cpp
)
target_link_libraries(clipbridge-desktop PRIVATE clipbridge_core)

include(CTest)
if(BUILD_TESTING)
    add_executable(clipbridge_core_tests
        tests/CoreTest.cpp
    )
    target_link_libraries(clipbridge_core_tests PRIVATE clipbridge_core Qt6::Test)
    add_test(NAME clipbridge_core_tests COMMAND clipbridge_core_tests)
    set_tests_properties(clipbridge_core_tests PROPERTIES
        ENVIRONMENT "QT_QPA_PLATFORM=offscreen")
endif()

install(TARGETS clipbridge-desktop RUNTIME DESTINATION bin)
