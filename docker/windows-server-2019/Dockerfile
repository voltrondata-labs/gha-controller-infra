FROM mcr.microsoft.com/windows/servercore:ltsc2019

WORKDIR /actions-runner

SHELL ["powershell", "-Command", "$ErrorActionPreference = 'Stop';$ProgressPreference='silentlyContinue';"]

RUN Invoke-WebRequest -Uri https://github.com/actions/runner/releases/download/v2.292.0/actions-runner-win-x64-2.292.0.zip -OutFile actions-runner-win-x64-2.292.0.zip

RUN if((Get-FileHash -Path actions-runner-win-x64-2.292.0.zip -Algorithm SHA256).Hash.ToUpper() -ne 'f27dae1413263e43f7416d719e0baf338c8d80a366fed849ecf5fffcec1e941f'.ToUpper()){ throw 'Computed checksum did not match' }

RUN Add-Type -AssemblyName System.IO.Compression.FileSystem ; [System.IO.Compression.ZipFile]::ExtractToDirectory('actions-runner-win-x64-2.292.0.zip', $PWD)

RUN Invoke-WebRequest -Uri 'https://aka.ms/install-powershell.ps1' -OutFile install-powershell.ps1; ./install-powershell.ps1 -AddToPath

RUN powershell Set-ExecutionPolicy Bypass -Scope Process -Force; [System.Net.ServicePointManager]::SecurityProtocol = [System.Net.ServicePointManager]::SecurityProtocol -bor 3072; iex ((New-Object System.Net.WebClient).DownloadString('https://community.chocolatey.org/install.ps1'))

RUN powershell choco install git.install --params "'/GitAndUnixToolsOnPath'" -y

RUN choco install msys2 --params "/NoUpdate" --version 20220904.0.0 -yv

RUN setx path '%path%;C:\tools\msys64\mingw64\bin;C:\tools\msys64\usr\bin;;%USERPROFILE%\.dotnet\tools;'

RUN pacman.exe -S --noconfirm --needed --noprogressbar cmake

RUN pacman.exe -S --noconfirm --needed --noprogressbar clang

RUN powershell choco feature enable -n allowGlobalConfirmation

COPY scripts /installation-scripts

COPY helpers /installation-helpers

RUN Get-ChildItem '/installation-scripts' | % {  & $_.FullName }

RUN powershell choco install python --version=3.7.9 

RUN cp C:\Python37\python.exe C:\Python37\python3.exe

RUN setx ImageOS 'win19'

RUN powershell choco install 7zip.install

CMD [ "powershell", "-c", "./config.cmd --name $env:RUNNER_NAME --url https://github.com/$env:RUNNER_REPO --token $env:RUNNER_TOKEN --labels $env:RUNNER_LABELS --unattended --replace --ephemeral; ./run.cmd"]
