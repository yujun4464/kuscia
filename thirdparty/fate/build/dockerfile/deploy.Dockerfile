#
# Copyright 2025 Ant Group Co., Ltd.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#   https://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#

FROM openanolis/anolisos:8.8

ARG ROOT_DIR="/data/projects"

RUN yum -y install wget xz sudo glibc openssl openssh-server openssh-clients net-tools gcc make zlib zlib-devel libffi-devel bzip2 lsof epel-release python2-pip numactl libncurses* libaio libaio-devel && \
    yum clean all

RUN mkdir -p ${ROOT_DIR}/etc && \
    mkdir -p ${ROOT_DIR}/scripts && \
    mkdir -p ${ROOT_DIR}/data && \
    wget https://webank-ai-1251170195.cos.ap-guangzhou.myqcloud.com/fate/1.11.1/release/fate_cluster_install_1.11.1_release.tar.gz && \
    tar -xzf fate_cluster_install_1.11.1_release.tar.gz -C ${ROOT_DIR} && \
    rm -rf fate_cluster_install_1.11.1_release.tar.gz && \
    groupadd -g 6000 apps && useradd -s /bin/bash -g apps -d /home/app app && \
    pushd /data && chown app:apps projects && chown -R app /data && popd

RUN wget -P ${ROOT_DIR}/data/example https://secretflow-data.oss-accelerate.aliyuncs.com/datasets/binary/test/mock/host.csv && \
    wget -P ${ROOT_DIR}/data/example https://secretflow-data.oss-accelerate.aliyuncs.com/datasets/binary/test/mock/guest.csv

COPY build/dockerfile/conf ${ROOT_DIR}/etc
COPY build/dockerfile/etc /etc
COPY build/dockerfile/scripts ${ROOT_DIR}/scripts

WORKDIR ${ROOT_DIR}
