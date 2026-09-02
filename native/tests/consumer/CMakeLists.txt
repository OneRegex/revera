# An external project that uses the installed Revera package.
# The parent build runs it from the revera_consumer test against a staging prefix.
# It also works by hand: cmake -S . -B build -DCMAKE_PREFIX_PATH=/path/to/prefix

cmake_minimum_required(VERSION 3.21)
project(revera_consumer LANGUAGES C CXX)

find_package(Revera CONFIG REQUIRED)

enable_testing()

add_executable(consumer_c main.c)
target_link_libraries(consumer_c PRIVATE Revera::C)
add_test(NAME consumer_c COMMAND consumer_c)

add_executable(consumer_cxx main.cpp)
target_link_libraries(consumer_cxx PRIVATE Revera::CXX)
add_test(NAME consumer_cxx COMMAND consumer_cxx)
