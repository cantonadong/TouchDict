fn main() {
    println!("cargo:rerun-if-changed=x64/WebView2Loader.dll");
    println!("cargo:rustc-link-lib=advapi32");
}
