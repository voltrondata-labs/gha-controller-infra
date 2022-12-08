FROM summerwind/actions-runner:latest

# install cmake and basic packages
RUN sudo apt-get update -y \
  && sudo apt-get upgrade -y \
  && sudo apt-get install -y build-essential=12.8ubuntu1.1 libssl-dev wget=1.20.3-1ubuntu2 ruby\
  && sudo apt-get clean all

RUN wget -O - https://apt.kitware.com/keys/kitware-archive-latest.asc 2>/dev/null | gpg --dearmor - | sudo tee /usr/share/keyrings/kitware-archive-keyring.gpg >/dev/null \
  && echo 'deb [signed-by=/usr/share/keyrings/kitware-archive-keyring.gpg] https://apt.kitware.com/ubuntu/ focal main' | sudo tee /etc/apt/sources.list.d/kitware.list >/dev/null \
  && sudo apt-get update -y \
  && sudo apt-get install cmake -y


# install r-base on ubuntu 20.04
RUN sudo apt-get update -y \
  && wget -qO- https://cloud.r-project.org/bin/linux/ubuntu/marutter_pubkey.asc | sudo tee -a /etc/apt/trusted.gpg.d/cran_ubuntu_key.asc \
  && sudo su -c "echo 'deb https://cloud.r-project.org/bin/linux/ubuntu focal-cran40/' > /etc/apt/sources.list.d/rlang.list" \
  && sudo apt-get update -y \
  && sudo apt-get install r-base -y

# Install docker-compose v1 from releases
RUN sudo curl -L "https://github.com/docker/compose/releases/download/1.29.2/docker-compose-Linux-x86_64" -o /usr/local/bin/docker-compose \
  && sudo chmod +x /usr/local/bin/docker-compose

RUN sudo gem install apt-dists-merge

# Add dotnet tools to PATH
ENV PATH="${PATH}:${HOME}/.dotnet/tools"
RUN echo "PATH=${PATH}" | sudo tee /etc/environment
