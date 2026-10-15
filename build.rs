fn main() {
    println!("cargo:rerun-if-changed=cmd/touchdict/TouchDict.exe.manifest");
    let mut resource = winresource::WindowsResource::new();
    resource.set("ProductName", "TouchDict");
    resource.set("FileDescription", "TouchDict · 英语学习词典");
    resource.set_icon_with_id("assets/touchdict.ico", "2");
    println!("cargo:rerun-if-changed=assets/touchdict.ico");
    resource.set_manifest_file("cmd/touchdict/TouchDict.exe.manifest");
    // LLVM's resource compiler can decode narrow UTF-8 strings as ANSI even
    // with code_page(65001). Use an ASCII-only wide literal for this value.
    let resource_path =
        std::path::PathBuf::from(std::env::var_os("OUT_DIR").unwrap()).join("touchdict.rc");
    resource.write_resource_file(&resource_path).unwrap();
    let description = "TouchDict · 英语学习词典";
    let wide_description: String = description
        .encode_utf16()
        .map(|unit| format!("\\x{unit:04x}"))
        .collect();
    let contents = std::fs::read_to_string(&resource_path).unwrap().replace(
        &format!("VALUE \"FileDescription\", \"{description}\""),
        &format!("VALUE \"FileDescription\", L\"{wide_description}\""),
    );
    std::fs::write(&resource_path, contents).unwrap();
    resource.set_resource_file(resource_path.to_str().unwrap());
    resource
        .compile()
        .expect("Windows resources could not be compiled");
}
