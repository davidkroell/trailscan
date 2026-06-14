build:
    mkdir -p bin
    go build -trimpath -o bin/ github.com/davidkroell/trailscan/cmd/trailscan
