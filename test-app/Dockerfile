# https://hub.docker.com/_/microsoft-dotnet
FROM mcr.microsoft.com/dotnet/sdk:5.0-focal AS build
WORKDIR /source

# copy csproj and restore as distinct layers
COPY *.csproj .
RUN dotnet restore

# copy everything else and build app
COPY * .
WORKDIR /source
RUN dotnet publish -c release -o /app --no-restore

# final stage/image
FROM mcr.microsoft.com/dotnet/aspnet:5.0-focal
WORKDIR /app
RUN apt-get update; apt-get install -y curl ca-certificates
COPY --from=build /app ./
COPY --from=build /source/LocalOTelCollectorTesting.cert /usr/local/share/ca-certificates/LocalOTelCollectorTesting.crt
COPY --from=build /source/LocalOTelCollectorTesting.cert /etc/ssl/certs/ca-certificates/LocalOTelCollectorTesting.crt
RUN update-ca-certificates
ENTRYPOINT ["dotnet", "test-app.dll"]
