// Argon2id for the browser pages, built to argon2.wasm by `make wasm`.
//
// The pages pass in every buffer, including Argon2's work area, so the module
// needs no allocator and no C library. It is built with bulk memory, so copies
// and fills compile to WebAssembly instructions rather than memcpy calls.
// Monocypher is vendored unchanged in monocypher/; see THIRD_PARTY_NOTICES.md.

#include "monocypher/monocypher.h"

// argon2id writes hash_size bytes of Argon2id (RFC 9106, version 0x13) to
// hash. work must hold blocks * 1024 bytes. key and ad may be empty.
__attribute__((export_name("argon2id")))
void argon2id(uint8_t *hash, uint32_t hash_size, void *work,
              uint32_t blocks, uint32_t passes, uint32_t lanes,
              const uint8_t *pass, uint32_t pass_size,
              const uint8_t *salt, uint32_t salt_size,
              const uint8_t *key, uint32_t key_size,
              const uint8_t *ad, uint32_t ad_size) {
	crypto_argon2_config config = {CRYPTO_ARGON2_ID, blocks, passes, lanes};
	crypto_argon2_inputs inputs = {pass, salt, pass_size, salt_size};
	crypto_argon2_extras extras = {key, ad, key_size, ad_size};
	crypto_argon2(hash, hash_size, work, config, inputs, extras);
}
