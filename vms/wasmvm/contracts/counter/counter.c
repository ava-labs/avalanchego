int count = 0;

int inc() {
    count++;
    return 0;
}

int getCount() {
    return count;
}

int add(int x) {
    count += x;
    return 0;
}

int dec() {
    count -= 1;
    return 0;
}

int sub(int x) {
    count -= x;
    return 0;
}

int main() {};