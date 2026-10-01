// oracle runs the vendored crates/wayle-cava/cava/src/cavacore.c on a
// deterministic signal and prints every frame's bars, one frame a line:
// the fixtures the Go port is held to. Regenerate with (fftw from nix):
//   gcc -O2 -I crates/wayle-cava/cava/include -I $FFTW_DEV/include \
//     cava/testdata/oracle.c crates/wayle-cava/cava/src/cavacore.c \
//     -L $FFTW/lib -lfftw3 -lm -o oracle
//   ./oracle 20 1 60 512 > cava/testdata/oracle_mono.txt
//   ./oracle 10 2 60 512 > cava/testdata/oracle_stereo.txt
#include <math.h>
#include <stdio.h>
#include <stdlib.h>
#include "cava/cavacore.h"

// signal is the test input: a rising chirp on the left, a falling one
// on the right, with a beating amplitude, all from integer arithmetic
// fed through sin.
static double signal_at(int ch, long i) {
    double t = (double)i / 44100.0;
    double f = ch == 0 ? 80.0 + 3000.0 * t : 4000.0 - 3000.0 * t;
    double amp = 8000.0 * (0.6 + 0.4 * sin(2.0 * M_PI * 1.3 * t));
    return amp * sin(2.0 * M_PI * f * t);
}

int main(int argc, char **argv) {
    int bars = atoi(argv[1]), channels = atoi(argv[2]), frames = atoi(argv[3]), chunk = atoi(argv[4]);
    struct cava_plan *p = cava_init(bars, 44100, channels, 1, 0.77, 50, 10000);
    if (p->status != 0) { fprintf(stderr, "%s\n", p->error_message); return 1; }
    double *in = malloc(sizeof(double) * chunk * channels);
    double *out = malloc(sizeof(double) * bars * channels);
    long i = 0;
    for (int f = 0; f < frames; f++) {
        // Every fifth frame delivers nothing, as a starved capture does.
        int n = (f % 5 == 4) ? 0 : chunk;
        for (int k = 0; k < n; k++, i++)
            for (int c = 0; c < channels; c++) in[k * channels + c] = signal_at(c, i);
        cava_execute(in, n * channels, out, p);
        for (int b = 0; b < bars * channels; b++) printf(b ? " %.17g" : "%.17g", out[b]);
        printf("\n");
    }
    cava_destroy(p);
    return 0;
}
