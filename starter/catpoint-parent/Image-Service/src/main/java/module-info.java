module Image.Service {
    exports com.udacity.imageService to Security.Service, application;
    requires java.desktop;
    requires software.amazon.awssdk.auth;
    requires software.amazon.awssdk.regions;
    requires software.amazon.awssdk.core;
    requires org.slf4j;
    requires software.amazon.awssdk.services.rekognition;
}