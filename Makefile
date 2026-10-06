.PHONY: build test vet lint install docs clean
build test vet lint install docs clean:
	$(MAKE) -C cli-go $@
