prefix=/usr
includedir=$(prefix)/include
libdir=$(prefix)/lib

CFLAGS ?= -g -fomit-frame-pointer -O2
CFLAGS += -Wall -I. -fPIC
SO_CFLAGS=-shared $(CFLAGS)
L_CFLAGS=$(CFLAGS)
LINK_FLAGS=
LINK_FLAGS+=$(LDFLAGS)
ENABLE_SHARED ?= 1

soname=libaio.so.1
minor=0
micro=1
libname=$(soname).$(minor).$(micro)
all_targets += $(OUT_DIR)/libaio.a

ifeq ($(ENABLE_SHARED),1)
all_targets += $(libname)
endif

all: $(all_targets)

# libaio provided functions
libaio_srcs := io_queue_init.c io_queue_release.c
libaio_srcs += io_queue_wait.c io_queue_run.c

# real syscalls
libaio_srcs += io_getevents.c io_submit.c io_cancel.c
libaio_srcs += io_setup.c io_destroy.c io_pgetevents.c

# internal functions
libaio_srcs += raw_syscall.c

# old symbols
libaio_srcs += compat-0_1.c

libaio_objs := $(patsubst %.c,$(OUT_DIR)/%.ol,$(libaio_srcs))
libaio_sobjs := $(patsubst %.c,$(OUT_DIR)/%.os,$(libaio_srcs))

$(libaio_objs) $(libaio_sobjs): libaio.h vsys_def.h

$(OUT_DIR)/%.os: %.c
	$(CC) $(SO_CFLAGS) -c -o $@ $<

$(OUT_DIR)/%.ol: %.c
	$(CC) $(L_CFLAGS) -c -o $@ $<

AR ?= ar
RANLIB ?= ranlib
$(OUT_DIR)/libaio.a: $(libaio_objs)
	rm -f $(OUT_DIR)/libaio.a
	$(AR) r $(OUT_DIR)/libaio.a $^
	$(RANLIB) $(OUT_DIR)/libaio.a

$(libname): $(libaio_sobjs) libaio.map
	$(CC) $(SO_CFLAGS) -Wl,--version-script=libaio.map -Wl,-soname=$(soname) -o $@ $(libaio_sobjs) $(LINK_FLAGS)

install: $(all_targets)
	install -D -m 644 libaio.h $(includedir)/libaio.h
	install -D -m 644 libaio.a $(libdir)/libaio.a
ifeq ($(ENABLE_SHARED),1)
	install -D -m 755 $(libname) $(libdir)/$(libname)
	ln -sf $(libname) $(libdir)/$(soname)
	ln -sf $(libname) $(libdir)/libaio.so
endif

$(libaio_objs): libaio.h

clean:
	rm -f $(all_targets) $(libaio_objs) $(libaio_sobjs) $(soname).new
	rm -f *.so* *.a *.o
