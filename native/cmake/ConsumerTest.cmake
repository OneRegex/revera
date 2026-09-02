# Runs as a CTest test with cmake -P.
# It installs the package into a staging prefix, builds tests/consumer against that prefix, and runs the consumer programs through their own ctest.
# Every step stops the script on failure.

foreach(name REVERA_BUILD_DIR REVERA_CONSUMER_SOURCE_DIR REVERA_STAGE_DIR REVERA_GENERATOR REVERA_C_COMPILER REVERA_CXX_COMPILER)
    if(NOT DEFINED ${name})
        message(FATAL_ERROR "${name} is not set")
    endif()
endforeach()

set(prefix "${REVERA_STAGE_DIR}/prefix")
set(consumer_build "${REVERA_STAGE_DIR}/build")
file(REMOVE_RECURSE "${REVERA_STAGE_DIR}")

# cmake takes --config for multi-configuration generators, and ctest spells the same thing -C.
set(config_args)
set(ctest_config_args)
if(REVERA_CONFIG)
    set(config_args --config "${REVERA_CONFIG}")
    set(ctest_config_args -C "${REVERA_CONFIG}")
endif()

execute_process(
    COMMAND "${CMAKE_COMMAND}" --install "${REVERA_BUILD_DIR}" --prefix "${prefix}" ${config_args}
    COMMAND_ERROR_IS_FATAL ANY)

execute_process(
    COMMAND "${CMAKE_COMMAND}"
        -S "${REVERA_CONSUMER_SOURCE_DIR}"
        -B "${consumer_build}"
        -G "${REVERA_GENERATOR}"
        "-DCMAKE_PREFIX_PATH=${prefix}"
        "-DCMAKE_C_COMPILER=${REVERA_C_COMPILER}"
        "-DCMAKE_CXX_COMPILER=${REVERA_CXX_COMPILER}"
        "-DCMAKE_BUILD_TYPE=${REVERA_CONFIG}"
    COMMAND_ERROR_IS_FATAL ANY)

execute_process(
    COMMAND "${CMAKE_COMMAND}" --build "${consumer_build}" ${config_args}
    COMMAND_ERROR_IS_FATAL ANY)

execute_process(
    COMMAND "${CMAKE_CTEST_COMMAND}" --test-dir "${consumer_build}" --output-on-failure ${ctest_config_args}
    COMMAND_ERROR_IS_FATAL ANY)
