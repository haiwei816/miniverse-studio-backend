# Local commands

Start: `docker compose -f deploy/compose.yaml up -d --build`

Stop without deleting data: `docker compose -f deploy/compose.yaml down`

Go checks without host Go installation:

```powershell
docker run --rm -v "${PWD}/backend:/src" -v miniverse_go_cache:/go -v miniverse_build_cache:/root/.cache/go-build -w /src golang:1.27.3 sh -c 'go test -race ./... && go vet ./...'
```

Compose uses a dedicated `miniverse-mvp` project and database volume. Development-only credentials are visible in Compose for reproducibility; do not expose this sandbox publicly. API listens on localhost:18080. No database port is published.

Windows managed environments may require an approved process to access Docker and localhost. Permission errors are environment failures and must not be recorded as successful tests.
