#include <stdarg.h>
#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>
#include <stdlib.h>


typedef struct Value {
  size_t len;
  const uint8_t *data;
} Value;

/**
 * A `KeyValue` struct that represents a key-value pair in the database.
 */
typedef struct KeyValue {
  struct Value key;
  struct Value value;
} KeyValue;

/**
 * Common arguments, accepted by both `fwd_create_db()` and `fwd_open_db()`.
 *
 * * `path` - The path to the database file, which will be truncated if passed to `fwd_create_db()`
 *   otherwise should exist if passed to `fwd_open_db()`.
 * * `cache_size` - The size of the node cache, panics if <= 0
 * * `revisions` - The maximum number of revisions to keep; firewood currently requires this to be at least 2
 */
typedef struct CreateOrOpenArgs {
  const char *path;
  size_t cache_size;
  size_t revisions;
  uint8_t strategy;
  uint16_t metrics_port;
} CreateOrOpenArgs;

/**
 * Puts the given key-value pairs into the database.
 *
 * # Returns
 *
 * The current root hash of the database, in Value form.
 *
 * # Safety
 *
 * This function is unsafe because it dereferences raw pointers.
 * The caller must:
 *  * ensure that `db` is a valid pointer returned by `open_db`
 *  * ensure that `values` is a valid pointer and that it points to an array of `KeyValue` structs of length `nkeys`.
 *  * ensure that the `Value` fields of the `KeyValue` structs are valid pointers.
 *
 */
struct Value fwd_batch(void *db,
                       size_t nkeys,
                       const struct KeyValue *values);

/**
 * Close and free the memory for a database handle
 *
 * # Safety
 *
 * This function uses raw pointers so it is unsafe.
 * It is the caller's responsibility to ensure that the database handle is valid.
 * Using the db after calling this function is undefined behavior
 *
 * # Arguments
 *
 * * `db` - The database handle to close, previously returned from a call to open_db()
 */
void fwd_close_db(void *db);

/**
 * Create a database with the given cache size and maximum number of revisions, as well
 * as a specific cache strategy
 *
 * # Arguments
 *
 * See `CreateOrOpenArgs`.
 *
 * # Returns
 *
 * A database handle, or panics if it cannot be created
 *
 * # Safety
 *
 * This function uses raw pointers so it is unsafe.
 * It is the caller's responsibility to ensure that path is a valid pointer to a null-terminated string.
 * The caller must also ensure that the cache size is greater than 0 and that the number of revisions is at least 2.
 * The caller must call `close` to free the memory associated with the returned database handle.
 *
 */
void *fwd_create_db(struct CreateOrOpenArgs args);

/**
 * Frees the memory associated with a `Value`.
 *
 * # Safety
 *
 * This function is unsafe because it dereferences raw pointers.
 * The caller must ensure that `value` is a valid pointer.
 */
void fwd_free_value(const struct Value *value);

/**
 * Gets the value associated with the given key from the database.
 *
 * # Arguments
 *
 * * `db` - The database handle returned by `open_db`
 *
 * # Safety
 *
 * The caller must:
 *  * ensure that `db` is a valid pointer returned by `open_db`
 *  * ensure that `key` is a valid pointer to a `Value` struct
 *  * call `free_value` to free the memory associated with the returned `Value`
 */
struct Value fwd_get(void *db, struct Value key);

/**
 * Open a database with the given cache size and maximum number of revisions
 *
 * # Arguments
 *
 * See `CreateOrOpenArgs`.
 *
 * # Returns
 *
 * A database handle, or panics if it cannot be created
 *
 * # Safety
 *
 * This function uses raw pointers so it is unsafe.
 * It is the caller's responsibility to ensure that path is a valid pointer to a null-terminated string.
 * The caller must also ensure that the cache size is greater than 0 and that the number of revisions is at least 2.
 * The caller must call `close` to free the memory associated with the returned database handle.
 *
 */
void *fwd_open_db(struct CreateOrOpenArgs args);

/**
 * Get the root hash of the latest version of the database
 * Don't forget to call `free_value` to free the memory associated with the returned `Value`.
 *
 * # Safety
 *
 * This function is unsafe because it dereferences raw pointers.
 * The caller must ensure that `db` is a valid pointer returned by `open_db`
 */
struct Value fwd_root_hash(void *db);
