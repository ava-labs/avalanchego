// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! Proc macros for Firewood metrics

use proc_macro::TokenStream;
use quote::{format_ident, quote};
use syn::parse::{Parse, ParseStream};
use syn::{ItemFn, ReturnType, parse_macro_input};

/// Arguments for the `#[metrics]` attribute: a single identifier naming a counter constant in
/// `crate::registry`. The corresponding histogram constant must also exist in `crate::registry`
/// under the name `{IDENT}_DURATION_SECONDS`.
struct MetricsArgs {
    ident: syn::Ident,
}

impl Parse for MetricsArgs {
    fn parse(input: ParseStream) -> syn::Result<Self> {
        let ident: syn::Ident = input.parse().map_err(|e| {
            syn::Error::new(
                e.span(),
                "expected a registry constant identifier, e.g., #[metrics(MY_OPERATION)]",
            )
        })?;
        if !input.is_empty() {
            return Err(syn::Error::new(
                input.span(),
                "unexpected argument; only one identifier is accepted \
                 — the description is declared in the registry doc comment",
            ));
        }
        Ok(MetricsArgs { ident })
    }
}

/// A proc macro attribute that automatically adds metrics instrumentation to functions.
///
/// Wraps a `Result`-returning function with:
/// - A counter `crate::registry::{IDENT}` labeled with `success = "true"` or `"false"`
/// - A histogram `crate::registry::{IDENT}_DURATION_SECONDS` recording the elapsed duration
///
/// Both constants must be declared in `crate::registry` via `firewood_metrics::define_metrics!`;
/// the compiler validates they exist.
///
/// # Usage
/// ```rust,ignore
/// use firewood_macros::metrics;
///
/// // Registry must declare PROPOSAL_COMMITS and PROPOSAL_COMMITS_DURATION_SECONDS
/// #[metrics(PROPOSAL_COMMITS)]
/// fn commit(...) -> Result<(), Error> {
///     // function body
/// }
/// ```
///
/// # Requirements
/// - The function must return a `Result<T, E>` type
/// - `crate::registry::{IDENT}` (counter) and `crate::registry::{IDENT}_DURATION_SECONDS`
///   (histogram) must both be declared in the calling crate's `registry` module
/// - The `metrics` crate must be available in the calling crate
#[proc_macro_attribute]
pub fn metrics(args: TokenStream, input: TokenStream) -> TokenStream {
    let input_fn = parse_macro_input!(input as ItemFn);

    if args.is_empty() {
        return syn::Error::new_spanned(
            &input_fn,
            "expected a registry constant identifier, e.g., #[metrics(MY_OPERATION)]",
        )
        .to_compile_error()
        .into();
    }

    let parsed_args = match syn::parse::<MetricsArgs>(args) {
        Ok(a) => a,
        Err(e) => return e.to_compile_error().into(),
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

    let expanded = generate_metrics_wrapper(&input_fn, &parsed_args.ident);
    TokenStream::from(expanded)
}

fn generate_metrics_wrapper(input_fn: &ItemFn, ident: &syn::Ident) -> proc_macro2::TokenStream {
    let fn_vis = &input_fn.vis;
    let fn_sig = &input_fn.sig;
    let fn_block = &input_fn.block;
    let fn_attrs = &input_fn.attrs;

    // Histogram constant: {IDENT}_DURATION_SECONDS — must be declared in crate::registry
    let duration_ident = format_ident!("{}_DURATION_SECONDS", ident);

    quote! {
        #(#fn_attrs)*
        #fn_vis #fn_sig {
            let __metrics_start = ::std::time::Instant::now();

            let __metrics_result = { #fn_block };

            // Use static label arrays to avoid runtime allocation
            static __METRICS_LABELS_SUCCESS: &[(&str, &str)] = &[("success", "true")];
            static __METRICS_LABELS_ERROR: &[(&str, &str)] = &[("success", "false")];
            let __metrics_labels = if __metrics_result.is_err() {
                __METRICS_LABELS_ERROR
            } else {
                __METRICS_LABELS_SUCCESS
            };

            ::firewood_metrics::firewood_counter!(#ident, __metrics_labels).increment(1);
            ::firewood_metrics::firewood_histogram!(#duration_ident).record(__metrics_start.elapsed().as_secs_f64());

            __metrics_result
        }
    }
}

#[cfg(test)]
mod tests {
    #![expect(clippy::unwrap_used)]

    use super::*;

    #[test]
    fn test_slow_proc_macro_compilation() {
        // Test that the proc macro generates compilable code
        let t = trybuild::TestCases::new();
        t.pass("tests/compile_pass/*.rs");
        t.compile_fail("tests/compile_fail/*.rs");
    }

    #[test]
    fn test_metrics_args_parsing() {
        // Test identifier parsing
        let input = quote::quote! { TEST_METRIC };
        let parsed: MetricsArgs = syn::parse2(input).unwrap();
        assert_eq!(parsed.ident.to_string(), "TEST_METRIC");
    }

    #[test]
    fn test_invalid_args_parsing() {
        // A literal number is not a valid identifier
        let input = quote::quote! { 123 };
        let result: syn::Result<MetricsArgs> = syn::parse2(input);
        assert!(result.is_err());

        // Extra arguments after the ident are not allowed
        let input = quote::quote! { MY_IDENT, extra };
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

        let ident: syn::Ident = syn::parse_str("TEST_METRIC").unwrap();
        let result = generate_metrics_wrapper(&input, &ident);
        let generated_code = result.to_string();

        // Verify key components are present in the generated code
        assert!(generated_code.contains("__METRICS_LABELS_SUCCESS"));
        assert!(generated_code.contains("__METRICS_LABELS_ERROR"));
        assert!(generated_code.contains("TEST_METRIC"));
        assert!(generated_code.contains("TEST_METRIC_DURATION_SECONDS"));
        assert!(
            generated_code.contains("std")
                && generated_code.contains("Instant")
                && generated_code.contains("now")
        );
        assert!(generated_code.contains("counter"));
        assert!(generated_code.contains("histogram"));
    }
}
