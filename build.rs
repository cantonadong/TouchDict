fn main() {
    println!("cargo:rerun-if-changed=cmd/touchdict/TouchDict.exe.manifest");
    let mut resource = winresource::WindowsResource::new();
    resource.set("ProductName", "TouchDict");
    resource.set("FileDescription", "TouchDict · 英语学习词典");
    resource.set_icon_with_id("assets/touchdict.ico", "2");
    println!("cargo:rerun-if-changed=assets/touchdict.ico");
    resource.set_manifest_file("cmd/touchdict/TouchDict.exe.manifest");
    resource
        .compile()
        .expect("Windows resources could not be compiled");
}
