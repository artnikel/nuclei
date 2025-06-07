package scanner

import (
    "bufio"
    "context"
    "os"
)

func ReadTargets(ctx context.Context, filepath string) (<-chan string, <-chan error) {
    out := make(chan string)
    errc := make(chan error, 1)

    go func() {
        defer close(out)
        file, err := os.Open(filepath)
        if err != nil {
            errc <- err
            close(errc)
            return
        }
        defer file.Close()

        scanner := bufio.NewScanner(file)

        for scanner.Scan() {
            select {
            case <-ctx.Done():
                errc <- ctx.Err()
                close(errc)
                return
            case out <- scanner.Text():
                // sent line to channel
            }
        }

        if err := scanner.Err(); err != nil {
            errc <- err
            close(errc)
            return
        }

        close(errc)
    }()

    return out, errc
}
