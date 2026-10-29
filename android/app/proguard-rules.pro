# kotlinx.serialization and Retrofit ship their own consumer rules; the
# API interface only needs its generic signatures kept.
-keepattributes Signature, InnerClasses, EnclosingMethod, *Annotation*
-keep,allowobfuscation,allowshrinking interface com.gooseberrydevelopment.backhog.api.BackhogApi

# ML Kit finds its components through Firebase's ComponentDiscovery, which
# instantiates each ComponentRegistrar named in the manifest by reflection.
# firebase-components' own rule keeps the classes but not their constructors,
# and R8 full mode then strips <init>(); the failure is swallowed, nothing gets
# registered, and TextRecognition/BarcodeScanning.getClient() hit a null.
-keep class * implements com.google.firebase.components.ComponentRegistrar { <init>(); }
