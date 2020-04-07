#include <stdlib.h>
#include "mylib.h"

int count = 0;

void inc() {
    count++;
}

int getCount() {
    return count;
}

void add(int x) {
    count += x;
}

void dec() {
    void *ctx;
    count = externalDec(count);
}

int main() {};