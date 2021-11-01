#!/bin/bash
#
# Script to run container-explorer tests on disk created
# by using generate-specimens.sh script from 
# https://github.com/dfirlabs/containerd-specimens
#

EXIT_SUCCESS=0
EXIT_FAILURE=1

# Display message
#
# Arguments:
#   an integer containing exit status code
#   a string containing message
#
display_message()
{
    local maxSize=90
    local exitStatus=$1
    local MESSAGE="$2"
    local padding=""

    size=${#MESSAGE}
    loopSize=`expr ${maxSize} - ${size}`

    if [ $size -lt $maxSize ]; then
        for i in $(seq 0 $loopSize)
        do
            padding+=" "
        done
    fi

    if [ ${exitStatus} -eq ${EXIT_SUCCESS} ]; then
        echo "${MESSAGE} ${padding} [   OK   ]"
    else
        echo "${MESSAGE} ${padding} [ FAILED ]"
    fi
   padding="" 
}

# Checks the availability of containerd namespace and exits if unavailable
#
# Arguments:
#   a string containing containerd namespace
#
assert_containerd_namespace()
{
    local NAMESPACE="$1"
    local exitStatus=${EXIT_SUCCESS}

    ns=`sudo go run "${CONTAINER_EXPLORER_PROGRAM}" -i "${MOUNT_POINT}" list namespaces | grep ${NAMESPACE} | tr -d '[:space:]'`
    if [ "${ns}" != "${NAMESPACE}" ]; then
        exitStatus=${EXIT_FAILURE}
    fi
    display_message ${exitStatus} "Checking containerd namespace ${NAMESPACE}"
}

# Checks the availability of containerd image and exits if unavailable
#
# Arguments:
#   a string containing containerd namespace
#   a string containing containerd image path
#
assert_containerd_image()
{
    local NAMESPACE=$1
    local IMAGE_PATH=$2
    local exitStatus=${EXIT_SUCCESS}

    imgpath=`sudo go run "${CONTAINER_EXPLORER_PROGRAM}" -n ${NAMESPACE} -i "${MOUNT_POINT}" list images | grep ${IMAGE_PATH} | awk '{print $2}' | tr -d '[:space:]'`
    if [ "${imgpath}" != "${IMAGE_PATH}" ]; then
        exitStatus=${EXIT_FAILURE}
    fi
    display_message ${exitStatus} "Checking containerd image ${IMAGE_PATH} for namespace ${NAMESPACE}"
}

# Checks the availability of containerd container name and exits if unavailable
#
# Arguments:
#   a string containing containerd namespace
#   a string contianing containerd container name
#
assert_containerd_container()
{
    local NAMESPACE=$1
    local CONTAINER_NAME=$2
    local exitStatus=${EXIT_SUCCESS}

    cn=`sudo go run "${CONTAINER_EXPLORER_PROGRAM}" -n ${NAMESPACE} -i "${MOUNT_POINT}" list containers | grep ${CONTAINER_NAME} | awk '{print $2}' | tr -d '[:space:]'`
    if [ "${cn}" != "${CONTAINER_NAME}" ]; then
        exitStatus=${EXIT_FAILURE}
    fi
    display_message ${exitStatus} "Checking containerd container ${CONTAINER_NAME} for namespace ${NAMESPACE}"
}

# Checks if a container is correctly mounted to a mount point
#
# This function assumes that a correctly mounted container must
# have /etc directory. The presence of /etc directory at mount
# point is considered a successful mount.
#
# Arguments:
#   a string containing containerd container name
#   a string containing mount point for containerd container
#
assert_container_mount_path()
{
    local CONTAINER_NAME=$1
    local CONTAINER_MOUNT_POINT="$2"
    local exitStatus=${EXIT_SUCCESS}

    # check the mounted container
    if [ ! -d "${CONTAINER_MOUNT_POINT}/etc" ]; then
        exitStatus=${EXIT_FAILURE}
    fi
    display_message ${exitStatus} "Checking container mount ${CONTAINER_NAME} at ${CONTAINER_MOUNT_POINT}"
}

# Checks that a containerd container is mounted at a given mount point
#
# Arguments:
#   a string containing containerd namespace
#   a string containing containerd container name
#   a string containing containerd container mount point
#
assert_containerd_mount()
{
    local NAMESPACE=$1
    local CONTAINER_NAME=$2
    local CONTAINER_MOUNT_POINT="$3"
    local exitStatus=${EXIT_SUCCESS}

    # create container mount point
    if [ ! -d "${CONTAINER_MOUNT_POINT}" ]; then
        sudo mkdir -p "${CONTAINER_MOUNT_POINT}"
    fi

    # mount the container
    sudo go run "${CONTAINER_EXPLORER_PROGRAM}" -n ${NAMESPACE} -i "${MOUNT_POINT}" mount ${CONTAINER_NAME} ${CONTAINER_MOUNT_POINT}

    assert_container_mount_path ${CONTAINER_NAME} "${CONTAINER_MOUNT_POINT}"
    # check the mounted container
    #if [ ! -d "${CONTAINER_MOUNT_POINT}/etc" ]; then
    #    exitStatus=${EXIT_FAILURE}
    #fi
    #display_message ${exitStatus} "Checking container mount ${CONTAINER_NAME} at ${CONTAINER_MOUNT_POINT}"

    # Clean up
    sudo umount ${CONTAINER_MOUNT_POINT} > /dev/null 2>&1
    sudo rm -rf ${CONTAINER_MOUNT_POINT} > /dev/null 2>&1
}

# Checks that all the available containers are mounted in subdirectory within
# the mount point
#
# Arguments:
#   a string containing containers mount point
#   a array of string containing containers name
#
assert_containerd_mount_all()
{
    local CONTAINER_MOUNT_POINT="$1"
    shift
    local CONTAINERS=("$@")

    local exitStatus=${EXIT_SUCCESS}

    # run container-explorer mount-all command
    sudo go run "${CONTAINER_EXPLORER_PROGRAM}" -i "${MOUNT_POINT}" mount-all "${CONTAINER_MOUNT_POINT}"

    sleep 5

    # check if containers mount point directory exits
    if [ ! -d "${CONTAINER_MOUNT_POINT}" ]; then
        exitStatus=${EXIT_FAILURE}
    fi
    display_message ${exitStatus} "Checking all containers' mount point ${CONTAINER_MOUNT_POINT}"


    for container in "${CONTAINERS[@]}"
    do
        assert_container_mount_path ${container} "${CONTAINER_MOUNT_POINT}/${container}"
    done
    # check mount point for nginx-specimen
    #if [ ! -d "${CONTAINER_MOUNT_POINT}/${NGINX_CONTAINER}/etc" ]; then
    #    exitStatus=${EXIT_FAILURE}
    #fi
    #display_message ${exitStatus} "Checking container mount point ${NGINX_CONTAINER} at ${CONTAINER_MOUNT_POINT}/${NGINX_CONTAINER}"

    # check mount point for redis-specimen
    #if [ ! -d "${CONTAINER_MOUNT_POINT}/${REDIS_CONTAINER}/etc" ]; then
    #    exitStatus=${EXIT_FAILURE}
    #fi
    #display_message ${exitStatus} "Checking contianer mount point ${REDIS_CONTAINER} at ${CONTAINER_MOUNT_POINT}/${REDIS_CONTAINER}"

    # Clean up
    sudo umount ${CONTAINER_MOUNT_POINT}/*
    sudo rm -rf ${CONTAINER_MOUNT_POINT}
}


# main
set -e

echo "Starting container-explorer test cases"

MOUNT_POINT="/mnt/container"
CONTAINER_ROOT="${MOUNT_POINT}/var/lib/containerd"
CONTAINER_EXPLORER_PROGRAM="main.go"
CONTAINERS=("nginx-specimen" "redis-specimen")

if [ ! -d "${MOUNT_POINT}" ]; then
    echo "Mount point ${MOUNT_POINT} does not exist"
    exit ${EXIT_FAILURE}
fi

if [ ! -d "${CONTAINER_ROOT}" ]; then
    echo "Contianerd root directory ${CONTAINER_ROOT} does not exist"
    exit ${EXIT_FAILURE}
fi

# Check containerd namespaces
assert_containerd_namespace default
assert_containerd_namespace dfirlabs
assert_containerd_namespace non-prod

# Check containerd images
assert_containerd_image default docker.io/library/nginx:latest
assert_containerd_image dfirlabs docker.io/library/redis:latest
assert_containerd_image prod docker.io/library/debian:buster

# Check containerd containers
assert_containerd_container default nginx-specimen
assert_containerd_container dfirlabs redis-specimen
assert_containerd_container prod debian-buster-specimen

# Check containerd mount
assert_containerd_mount default nginx-specimen /tmp/mnt/nginx-specimen

# Check all containers mount
assert_containerd_mount_all /tmp/mnt/containers ${CONTAINERS[@]}
