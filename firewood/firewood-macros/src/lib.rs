// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! Proc macros for Firewood metrics

use proc_macro::TokenStream;
use quote::quote;
use syn::parse::{Parse, ParseStream};
use syn::{ItemFn, Lit, ReturnType, Token, parse_macro_input};

/// Arguments for the metrics macro
struct MetricsArgs {
    name: String,
    description: Option<String>,
}

impl Parse for MetricsArgs {
    fn parse(input: ParseStream) -> syn::Result<Self> {
        let name_lit: Lit = input.parse()?;
        let name = match name_lit {
            Lit::Str(s) => s.value(),
            _ => {
                return Err(syn::Error::new_spanned(
                    name_lit,
                    "Expected string literal for metric name",
                ));
            }
        };

        let description = if input.parse::<Token![,]>().is_ok() {
            let desc_lit: Lit = input.parse()?;
            match desc_lit {
                Lit::Str(s) => Some(s.value()),
                _ => {
                    return Err(syn::Error::new_spanned(
                        desc_lit,
                        "Expected string literal for description",
                    ));
                }
            }
        } else {
            None
        };

        Ok(MetricsArgs { name, description })
    }
}

/// A proc macro attribute that automatically adds metrics timing to functions.
///
/// This macro adds timing instrumentation to functions that return `Result<T, E>`.
/// It generates two counters:
/// 1. A count counter with the provided prefix that increments by 1
/// 2. A timing counter with the prefix + "_ms" that records elapsed time in milliseconds
///
/// Both counters include a "success" label that is "true" for Ok results and "false" for Err results.
/// The metrics are automatically registered with descriptions for better observability.
///
/// # Usage
/// ```rust,ignore
/// use firewood_macros::metrics;
///
/// // Basic usage with just a metric name
/// #[metrics("my.operation")]
/// fn my_function() -> Result<String, &'static str> {
///     // function body
///     Ok("success".to_string())
/// }
///
/// // With an optional description
/// #[metrics("my.operation", "Description of what this operation does")]
/// fn my_function_with_desc() -> Result<String, &'static str> {
///     // function body
///     Ok("success".to_string())
/// }
/// ```
///
/// # Generated Code
/// The macro transforms the function to include:
/// - Metric registration with descriptions at the beginning
/// - A timer start at the beginning of the function
/// - Metrics collection before returning, with success/failure labels
/// - Preservation of the original return value
///
/// # Requirements
/// - The function must return a `Result<T, E>` type
/// - The `metrics` and `coarsetime` crates must be available
#[proc_macro_attribute]
pub fn metrics(args: TokenStream, input: TokenStream) -> TokenStream {
    let input_fn = parse_macro_input!(input as ItemFn);

    // Parse the attribute arguments - expecting metric name and optional description
    let (metric_prefix, description) = if args.is_empty() {
        return syn::Error::new_spanned(
            &input_fn,
            "Expected string literal for metric prefix, e.g., #[metrics(\"my.operation\")] or #[metrics(\"my.operation\", \"description\")]",
        )
        .to_compile_error()
        .into();
    } else {
        match syn::parse::<MetricsArgs>(args) {
            Ok(parsed_args) => (parsed_args.name, parsed_args.description),
            Err(e) => {
                return syn::Error::new(
                    e.span(),
                    "Expected string literal(s) for metric name and optional description",
                )
                .to_compile_error()
                .into();
            }
        }
    };

    // Validate that the function returns a Result
    let return_type = match &input_fn.sig.output {
        ReturnType::Type(_, ty) => ty,
        ReturnType::Default => {
            return syn::Error::new_spanned(
                &input_fn.sig,
                "Function must return a Result<T, E> to use #[metrics] attribute",
            )
            .to_compile_error()
            .into();
        }
    };

    // Check if it's a Result type (this is a simple check, could be more sophisticated)
    let is_result = match return_type.as_ref() {
        syn::Type::Path(type_path) => type_path
            .path
            .segments
            .last()
            .is_some_and(|seg| seg.ident == "Result"),
        _ => false,
    };

    if !is_result {
        return syn::Error::new_spanned(
            return_type,
            "Function must return a Result<T, E> to use #[metrics] attribute",
        )
        .to_compile_error()
        .into();
    }

    let args = MetricsArgs {
        name: metric_prefix,
        description,
    };

    let expanded = generate_metrics_wrapper(&input_fn, &args);
    TokenStream::from(expanded)
}

fn generate_metrics_wrapper(input_fn: &ItemFn, args: &MetricsArgs) -> proc_macro2::TokenStream {
    let fn_vis = &input_fn.vis;
    let fn_sig = &input_fn.sig;
    let fn_block = &input_fn.block;
    let fn_attrs = &input_fn.attrs;
    let metric_prefix = &args.name;

    // Generate description registration code if description is provided
    let registration_code = if let Some(desc) = &args.description {
        let count_desc = format!("Number of {desc} operations");
        let timing_desc = format!("Timing of {desc} operations in milliseconds");
        quote! {
            // Register metrics with descriptions (only runs once due to static guard)
            static __METRICS_REGISTERED: std::sync::Once = std::sync::Once::new();
            __METRICS_REGISTERED.call_once(|| {
                metrics::describe_counter!(#metric_prefix, #count_desc);
                metrics::describe_counter!(concat!(#metric_prefix, "_ms"), #timing_desc);
            });
        }
    } else {
        quote! {
            // Register metrics without descriptions (only runs once due to static guard)
            static __METRICS_REGISTERED: std::sync::Once = std::sync::Once::new();
            __METRICS_REGISTERED.call_once(|| {
                metrics::describe_counter!(#metric_prefix, "Operation counter");
                metrics::describe_counter!(concat!(#metric_prefix, "_ms"), "Operation timing in milliseconds");
            });
        }
    };

    quote! {
        #(#fn_attrs)*
        #fn_vis #fn_sig {
            #registration_code

            let __metrics_start = coarsetime::Instant::now();

            let __metrics_result = { #fn_block };

            // Use static label arrays to avoid runtime allocation
            static __METRICS_LABELS_SUCCESS: &[(&str, &str)] = &[("success", "true")];
            static __METRICS_LABELS_ERROR: &[(&str, &str)] = &[("success", "false")];
            let __metrics_labels = if __metrics_result.is_err() {
                __METRICS_LABELS_ERROR
            } else {
                __METRICS_LABELS_SUCCESS
            };

            // Increment count counter (base name)
            metrics::counter!(#metric_prefix, __metrics_labels).increment(1);

            // Increment timing counter (base name + "_ms") using compile-time concatenation
            metrics::counter!(concat!(#metric_prefix, "_ms"), __metrics_labels)
                .increment(__metrics_start.elapsed().as_millis());

            __metrics_result
        }
    }
}

#[cfg(test)]
mod tests {
    #![expect(clippy::unwrap_used)]

    use super::*;

    #[test]
    fn test_proc_macro_compilation() {
        // Test that the proc macro generates compilable code
        let t = trybuild::TestCases::new();
        t.pass("tests/compile_pass/*.rs");
        t.compile_fail("tests/compile_fail/*.rs");
    }

    #[test]
    fn test_metrics_args_parsing() {
        // Test single argument parsing
        let input = quote::quote! { "test.metric" };
        let parsed: MetricsArgs = syn::parse2(input).unwrap();
        assert_eq!(parsed.name, "test.metric");
        assert_eq!(parsed.description, None);

        // Test two argument parsing
        let input = quote::quote! { "test.metric", "test description" };
        let parsed: MetricsArgs = syn::parse2(input).unwrap();
        assert_eq!(parsed.name, "test.metric");
        assert_eq!(parsed.description, Some("test description".to_string()));
    }

    #[test]
    fn test_invalid_args_parsing() {
        // Test that invalid arguments fail to parse
        let input = quote::quote! { 123 };
        let result: syn::Result<MetricsArgs> = syn::parse2(input);
        assert!(result.is_err());

        // Test that too many arguments fail
        let input = quote::quote! { "test.metric", "description", "extra" };
        let result: syn::Result<MetricsArgs> = syn::parse2(input);
        assert!(result.is_err());
    }

    #[test]
    fn test_generated_code_structure() {
        // Test that the proc macro generates the expected code structure
        use syn::{ItemFn, parse_quote};

        let input: ItemFn = parse_quote! {
            fn test_function() -> Result<(), &'static str> {
                Ok(())
            }
        };

        let args = MetricsArgs {
            name: "test.metric".to_string(),
            description: Some("test description".to_string()),
        };

        let result = generate_metrics_wrapper(&input, &args);
        let generated_code = result.to_string();

        // Verify key components are present in the generated code
        assert!(generated_code.contains("__METRICS_LABELS_SUCCESS"));
        assert!(generated_code.contains("__METRICS_LABELS_ERROR"));
        assert!(generated_code.contains("test.metric"));
        assert!(
            generated_code.contains("coarsetime")
                && generated_code.contains("Instant")
                && generated_code.contains("now")
        );
        assert!(generated_code.contains("metrics") && generated_code.contains("counter"));
        assert!(generated_code.contains("Number of test description operations"));
        assert!(generated_code.contains("Timing of test description operations"));

        // Check for the _ms suffix - it should be generated by concat!
        assert!(generated_code.contains("concat") && generated_code.contains('!'));
        assert!(generated_code.contains("_ms"));
    }
}
